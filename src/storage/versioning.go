package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	bolt "go.etcd.io/bbolt"
)

// Version status values for bucket versioning (S3 semantics); the buckets
// themselves are declared in storage.go's bucket registry.
const (
	VersioningEnabled   = "Enabled"
	VersioningSuspended = "Suspended"
)

// VersionRecord describes one version of an object in a versioned bucket.
// The content itself stays in the CAS layer (dedup by hash); a record only
// points at it, so N versions of identical content cost one physical blob.
type VersionRecord struct {
	VersionID      string `json:"version_id"`
	Hash           string `json:"hash,omitempty"` // empty for delete markers
	Size           int64  `json:"size"`
	Timestamp      int64  `json:"timestamp"` // unix nano (write time)
	IsLatest       bool   `json:"is_latest"`
	IsDeleteMarker bool   `json:"is_delete_marker,omitempty"`

	// Key is the object key this record belongs to. Not serialized — the
	// composite bucket key already encodes it; ListVersions/LatestVersion
	// populate it for callers (S3 XML rendering).
	Key string `json:"-"`
}

// VersionPage is one page of ListVersions output.
type VersionPage struct {
	Records       []VersionRecord
	NextKeyMarker string
	NextVersionID string
	IsTruncated   bool
}

// versionKey builds the composite object_versions key.
func versionKey(bucket, key, versionID string) []byte {
	return []byte(bucket + "\x00" + key + "\x00" + versionID)
}

// newVersionID returns an opaque S3-style version id (UUIDv4).
func newVersionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("failed to generate version id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst), nil
}

// SetBucketVersioning sets the versioning status ("Enabled"/"Suspended") for a
// bucket. Once enabled, versioning can be suspended but never removed.
func (s *CASStore) SetBucketVersioning(bucket, status string) (err error) {
	defer common.RecoverErr("CASStore.SetBucketVersioning", &err)
	if status != VersioningEnabled && status != VersioningSuspended {
		return fmt.Errorf("invalid versioning status %q: %w", status, syscall.EINVAL)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketVersioning).Put([]byte(bucket), []byte(status))
	})
}

// BucketVersioning returns the versioning status for a bucket and whether any
// status has been configured.
func (s *CASStore) BucketVersioning(bucket string) (status string, configured bool, err error) {
	defer common.RecoverErr("CASStore.BucketVersioning", &err)
	s.mu.RLock()
	defer s.mu.RUnlock()
	err = s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketVersioning).Get([]byte(bucket))
		if v == nil {
			return nil
		}
		status = string(v)
		configured = true
		return nil
	})
	return status, configured, err
}

// IsVersioned reports whether the bucket has versioning enabled.
func (s *CASStore) IsVersioned(bucket string) (enabled bool, err error) {
	status, configured, err := s.BucketVersioning(bucket)
	if err != nil {
		return false, err
	}
	return configured && status == VersioningEnabled, nil
}

// RecordVersion appends a data version for (bucket, key) pointing at the CAS
// blob hash and marks it latest. Every PUT to a versioned bucket creates a new
// version even for identical content — S3 mutation identity.
func (s *CASStore) RecordVersion(bucket, key, hash string, size int64) (versionID string, err error) {
	defer common.RecoverErr("CASStore.RecordVersion", &err)
	versionID, err = newVersionID()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.db.Update(func(tx *bolt.Tx) error {
		return putVersion(tx, bucket, key, VersionRecord{
			VersionID: versionID,
			Hash:      hash,
			Size:      size,
			Timestamp: time.Now().UnixNano(),
			IsLatest:  true,
		})
	})
	if err != nil {
		return "", err
	}
	return versionID, nil
}

// RecordDeleteMarker appends a delete marker as the newest version of
// (bucket, key). The object appears deleted for plain GETs until the marker is
// removed or a newer version is written.
func (s *CASStore) RecordDeleteMarker(bucket, key string) (versionID string, err error) {
	defer common.RecoverErr("CASStore.RecordDeleteMarker", &err)
	versionID, err = newVersionID()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.db.Update(func(tx *bolt.Tx) error {
		return putVersion(tx, bucket, key, VersionRecord{
			VersionID:      versionID,
			Timestamp:      time.Now().UnixNano(),
			IsLatest:       true,
			IsDeleteMarker: true,
		})
	})
	if err != nil {
		return "", err
	}
	return versionID, nil
}

// putVersion inserts a record and re-points IsLatest to it. Caller holds s.mu.
func putVersion(tx *bolt.Tx, bucket, key string, rec VersionRecord) error {
	b := tx.Bucket(bucketObjectVersions)
	prefix := []byte(bucket + "\x00" + key + "\x00")
	c := b.Cursor()
	for k, v := c.Seek(prefix); k != nil && string(k[:len(prefix)]) == string(prefix); k, v = c.Next() {
		var old VersionRecord
		if err := json.Unmarshal(v, &old); err != nil {
			return fmt.Errorf("corrupt version record: %w", err)
		}
		if old.IsLatest {
			old.IsLatest = false
			nv, err := json.Marshal(old)
			if err != nil {
				return err
			}
			if err := b.Put(k, nv); err != nil {
				return err
			}
		}
	}
	nv, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return b.Put(versionKey(bucket, key, rec.VersionID), nv)
}

// latestVersionTx finds the newest record for a key inside a live view.
// Caller must hold s.mu.
func latestVersionTx(tx *bolt.Tx, bucket, key string) (rec VersionRecord, ok bool, err error) {
	b := tx.Bucket(bucketObjectVersions)
	prefix := []byte(bucket + "\x00" + key + "\x00")
	c := b.Cursor()
	var best *VersionRecord
	for k, v := c.Seek(prefix); k != nil && string(k[:len(prefix)]) == string(prefix); k, v = c.Next() {
		var r VersionRecord
		if err := json.Unmarshal(v, &r); err != nil {
			return rec, false, fmt.Errorf("corrupt version record: %w", err)
		}
		if best == nil || r.Timestamp > best.Timestamp || (r.Timestamp == best.Timestamp && r.VersionID > best.VersionID) {
			tmp := r
			best = &tmp
		}
	}
	if best == nil {
		return rec, false, nil
	}
	best.Key = key
	return *best, true, nil
}

// LatestVersion returns the newest record (data version or delete marker) for
// (bucket, key). ok=false when the key has no versions.
func (s *CASStore) LatestVersion(bucket, key string) (rec VersionRecord, ok bool, err error) {
	defer common.RecoverErr("CASStore.LatestVersion", &err)
	s.mu.RLock()
	defer s.mu.RUnlock()
	err = s.db.View(func(tx *bolt.Tx) error {
		var viewErr error
		rec, ok, viewErr = latestVersionTx(tx, bucket, key)
		return viewErr
	})
	return rec, ok, err
}

// GetVersion returns one specific version record.
func (s *CASStore) GetVersion(bucket, key, versionID string) (rec VersionRecord, err error) {
	defer common.RecoverErr("CASStore.GetVersion", &err)
	s.mu.RLock()
	defer s.mu.RUnlock()
	err = s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketObjectVersions).Get(versionKey(bucket, key, versionID))
		if v == nil {
			return syscall.ENOENT
		}
		if err := json.Unmarshal(v, &rec); err != nil {
			return fmt.Errorf("corrupt version record: %w", err)
		}
		rec.Key = key
		return nil
	})
	if err == syscall.ENOENT {
		return VersionRecord{}, syscall.ENOENT
	}
	return rec, err
}

// versionEntry pairs a record with its object key during listing.
type versionEntry struct {
	key string
	rec VersionRecord
}

// ListVersions returns versions for keys under prefix in S3 order (key asc,
// last-modified desc within key), paginated by (keyMarker, versionIDMarker).
// maxKeys <= 0 means unlimited.
func (s *CASStore) ListVersions(bucket, prefix, keyMarker, versionIDMarker string, maxKeys int) (page VersionPage, err error) {
	defer common.RecoverErr("CASStore.ListVersions", &err)
	s.mu.RLock()
	defer s.mu.RUnlock()

	err = s.db.View(func(tx *bolt.Tx) error {
		entries, scanErr := scanVersionEntries(tx, bucket, prefix)
		if scanErr != nil {
			return scanErr
		}
		sortVersionEntries(entries)
		entries = resumeAfterMarker(entries, keyMarker, versionIDMarker)
		return paginate(entries, maxKeys, &page)
	})
	return page, err
}

// scanVersionEntries collects every version record of the bucket, carrying its
// object key. It always scans from the bucket start: version keys sort by
// random UUID, so a seek to the marker key would skip the marker key's own
// older records. Caller must hold s.mu.
func scanVersionEntries(tx *bolt.Tx, bucket, prefix string) ([]versionEntry, error) {
	b := tx.Bucket(bucketObjectVersions)
	head := bucket + "\x00"
	c := b.Cursor()
	var entries []versionEntry
	for k, v := c.Seek([]byte(head)); k != nil; k, v = c.Next() {
		full := string(k)
		if len(full) <= len(head) || full[:len(head)] != head {
			break // past this bucket's keys
		}
		rest := full[len(head):]
		nul := strings.IndexByte(rest, 0)
		if nul < 0 {
			continue // malformed; skip
		}
		key := rest[:nul]
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}
		var r VersionRecord
		if err := json.Unmarshal(v, &r); err != nil {
			return nil, fmt.Errorf("corrupt version record: %w", err)
		}
		entries = append(entries, versionEntry{key: key, rec: r})
	}
	return entries, nil
}

// resumeAfterMarker drops every entry at or before the (keyMarker,
// versionIDMarker) resume point in S3 order. Version ids are random UUIDs and
// carry no order, so the marker position is only meaningful after sorting —
// callers invoke this AFTER sortVersionEntries. When the marker record was
// pruned, resume at the first entry strictly after the marker key.
func resumeAfterMarker(entries []versionEntry, keyMarker, versionIDMarker string) []versionEntry {
	if keyMarker == "" || versionIDMarker == "" {
		return entries
	}
	for i, e := range entries {
		if e.key == keyMarker && e.rec.VersionID == versionIDMarker {
			return entries[i+1:]
		}
		if e.key > keyMarker {
			return entries[i:]
		}
	}
	return nil // nothing after the marker key
}

// sortVersionEntries orders entries key asc, last-modified desc within key.
func sortVersionEntries(entries []versionEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].key != entries[j].key {
			return entries[i].key < entries[j].key
		}
		if entries[i].rec.Timestamp != entries[j].rec.Timestamp {
			return entries[i].rec.Timestamp > entries[j].rec.Timestamp
		}
		return entries[i].rec.VersionID > entries[j].rec.VersionID
	})
}

// paginate trims to maxKeys and fills the page's truncation markers.
func paginate(entries []versionEntry, maxKeys int, page *VersionPage) error {
	if maxKeys > 0 && len(entries) > maxKeys {
		entries = entries[:maxKeys]
		page.IsTruncated = true
	}
	page.Records = make([]VersionRecord, len(entries))
	for i, e := range entries {
		e.rec.Key = e.key
		page.Records[i] = e.rec
	}
	if page.IsTruncated && len(entries) > 0 {
		last := entries[len(entries)-1]
		page.NextKeyMarker = last.key
		page.NextVersionID = last.rec.VersionID
	}
	return nil
}

// DeleteVersion permanently removes one version record. If the removed record
// was latest, IsLatest re-points to the next-newest record (which may be a
// delete marker, reviving the object per S3 semantics). The returned record is
// the deleted one so the caller can decide on blob cleanup.
func (s *CASStore) DeleteVersion(bucket, key, versionID string) (deleted VersionRecord, err error) {
	defer common.RecoverErr("CASStore.DeleteVersion", &err)
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketObjectVersions)
		k := versionKey(bucket, key, versionID)
		v := b.Get(k)
		if v == nil {
			return syscall.ENOENT
		}
		if err := json.Unmarshal(v, &deleted); err != nil {
			return fmt.Errorf("corrupt version record: %w", err)
		}
		if err := b.Delete(k); err != nil {
			return err
		}
		return rePointLatest(tx, bucket, key, deleted.IsLatest)
	})
	return deleted, err
}

// rePointLatest re-points IsLatest to the next-newest record after the latest
// one was removed. noOp when the removed record wasn't latest or the key is
// now empty. Caller must hold s.mu inside a live update.
func rePointLatest(tx *bolt.Tx, bucket, key string, wasLatest bool) error {
	if !wasLatest {
		return nil
	}
	next, ok, err := latestVersionTx(tx, bucket, key)
	if err != nil || !ok {
		return err
	}
	if next.IsLatest {
		return nil
	}
	next.IsLatest = true
	nv, err := json.Marshal(next)
	if err != nil {
		return err
	}
	return tx.Bucket(bucketObjectVersions).Put(versionKey(bucket, key, next.VersionID), nv)
}

// GetVersionContent opens the content stream for one specific version.
// Delete markers have no content and return syscall.ENOENT.
func (s *CASStore) GetVersionContent(bucket, key, versionID string) (rc io.ReadCloser, rec VersionRecord, err error) {
	defer common.RecoverErr("CASStore.GetVersionContent", &err)
	rec, err = s.GetVersion(bucket, key, versionID)
	if err != nil {
		return nil, VersionRecord{}, err
	}
	if rec.IsDeleteMarker {
		return nil, rec, syscall.ENOENT
	}
	rc, err = s.blobs.GetBlob(rec.Hash)
	if err != nil {
		return nil, rec, err
	}
	return rc, rec, nil
}

// RepointNamespace points the flat namespace mapping for name at hash (empty
// hash removes the mapping) — used by versioned delete/revive paths so plain
// (non-versioned) GETs follow the latest data version.
func (s *CASStore) RepointNamespace(name, hash string) (err error) {
	defer common.RecoverErr("CASStore.RepointNamespace", &err)
	s.mu.Lock()
	defer s.mu.Unlock()
	if hash == "" {
		return s.db.Update(func(tx *bolt.Tx) error {
			return tx.Bucket(bucketNamespace).Delete([]byte(name))
		})
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketNamespace).Put([]byte(name), []byte(hash))
	})
}
