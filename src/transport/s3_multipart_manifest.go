package transport

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// multipartManifestName is the per-upload manifest inside its spill directory. It
// is what makes an in-flight upload survive a daemon restart (issue #1371).
const multipartManifestName = "manifest.json"

// multipartManifest is the durable record of an open upload. It holds only what
// cannot be re-derived from the part files on disk.
type multipartManifest struct {
	Version   int                `json:"version"`
	UploadID  string             `json:"upload_id"`
	Bucket    string             `json:"bucket"`
	Key       string             `json:"key"`
	CreatedAt time.Time          `json:"created_at"`
	Parts     []multipartPartRec `json:"parts"`
}

// multipartPartRec is one part as persisted; the path is derived from the number so
// a spill directory moved between nodes still reads correctly.
type multipartPartRec struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
	Size       int64  `json:"size"`
}

// writeManifest persists the session's state into its spill directory. It is
// called after a part file is in place, so the manifest never advertises a part
// that does not exist. Failures are logged rather than failing the request: the
// upload is still usable in this process, only its restart-recovery is lost.
func writeManifest(uploadID string, up *multipartUpload) {
	up.mu.Lock()
	dir := up.spillDir
	if dir == "" {
		up.mu.Unlock()
		return
	}
	manifest := multipartManifest{
		Version:   1,
		UploadID:  uploadID,
		Bucket:    up.bucket,
		Key:       up.key,
		CreatedAt: up.createdAt,
		Parts:     make([]multipartPartRec, 0, len(up.parts)),
	}
	for _, p := range up.parts {
		manifest.Parts = append(manifest.Parts, multipartPartRec{
			PartNumber: p.partNumber, ETag: p.etag, Size: p.size,
		})
	}
	up.mu.Unlock()

	data, err := json.Marshal(manifest)
	if err != nil {
		log.Printf("AUDIT: multipart manifest marshal failed for %s: %v", uploadID, err)
		return
	}
	if err := writeAtomic(filepath.Join(dir, multipartManifestName), data); err != nil {
		log.Printf("AUDIT: multipart manifest write failed for %s: %v", uploadID, err)
	}
}

// writeAtomic writes data to path via a temporary file in the same directory and
// a rename, so a crash mid-write cannot leave a truncated manifest behind.
func writeAtomic(path string, data []byte) (err error) {
	defer common.RecoverErr("multipart manifest write", &err)

	tmp := path + ".tmp"
	f, cerr := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if cerr != nil {
		return cerr
	}
	if _, werr := f.Write(data); werr != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return werr
	}
	if serr := f.Sync(); serr != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return serr
	}
	if clErr := f.Close(); clErr != nil {
		_ = os.Remove(tmp)
		return clErr
	}
	if rerr := os.Rename(tmp, path); rerr != nil {
		_ = os.Remove(tmp)
		return rerr
	}
	return nil
}

// RecoverMultipartSessions rebuilds the in-memory session map from the manifests
// left in the spill root by a previous process, so a client's in-flight uploadId
// keeps working across a restart. Sessions older than ttl are skipped: expiry
// still governs, and leaving them to the reaper keeps one policy in one place.
// Returns how many sessions were recovered.
func RecoverMultipartSessions(ttl time.Duration, now time.Time) (recovered int) {
	root, err := multipartSpillRoot()
	if err != nil {
		log.Printf("AUDIT: multipart recovery skipped (no spill root): %v", err)
		return 0
	}
	entries, rerr := os.ReadDir(root)
	if rerr != nil {
		log.Printf("AUDIT: multipart recovery cannot read %s: %v", root, rerr)
		return 0
	}

	for _, entry := range entries {
		// Lstat semantics: Readdir already reports symlinks as symlinks, and a
		// symlinked directory is never one of ours.
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		up, id, ok := loadManifest(dir, ttl, now)
		if !ok {
			continue
		}
		muUploads.Lock()
		if existing, dup := uploads[id]; dup {
			// A live session already owns this id (recovery ran twice); keep the
			// live one.
			muUploads.Unlock()
			_ = existing
			continue
		}
		uploads[id] = up
		muUploads.Unlock()
		recovered++
	}
	if recovered > 0 {
		log.Printf("AUDIT: recovered %d in-flight multipart upload(s) after restart", recovered)
	}
	return recovered
}

// loadManifest reads and validates one spill directory's manifest. A directory
// that is not a usable session is reported as ok=false and left alone, so the
// reaper can expire it on its own schedule.
func loadManifest(dir string, ttl time.Duration, now time.Time) (*multipartUpload, string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, multipartManifestName))
	if err != nil {
		return nil, "", false
	}
	var manifest multipartManifest
	if jerr := json.Unmarshal(data, &manifest); jerr != nil {
		log.Printf("AUDIT: multipart manifest in %s is unreadable: %v", dir, jerr)
		return nil, "", false
	}
	if manifest.UploadID == "" || manifest.Bucket == "" || manifest.Key == "" {
		log.Printf("AUDIT: multipart manifest in %s is incomplete; not recovering", dir)
		return nil, "", false
	}
	if ttl > 0 && manifest.CreatedAt.Before(now.Add(-ttl)) {
		return nil, "", false
	}

	parts := make([]multipartPart, 0, len(manifest.Parts))
	for _, rec := range manifest.Parts {
		path := filepath.Join(dir, strconv.Itoa(rec.PartNumber)+".part")
		info, serr := os.Stat(path)
		if serr != nil || info.Size() != rec.Size {
			// The manifest claims a part the disk does not have (or has at a
			// different size): the session cannot be completed faithfully.
			log.Printf("AUDIT: multipart %s part %d is missing or truncated; not recovering", manifest.UploadID, rec.PartNumber)
			return nil, "", false
		}
		parts = append(parts, multipartPart{
			partNumber: rec.PartNumber, etag: rec.ETag, size: rec.Size, path: path,
		})
	}

	return &multipartUpload{
		bucket:    manifest.Bucket,
		key:       manifest.Key,
		createdAt: manifest.CreatedAt,
		spillDir:  dir,
		parts:     parts,
	}, manifest.UploadID, true
}
