package storage

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"bytes"
	"crypto/sha256"

	"go.etcd.io/bbolt"
	"golang.org/x/sys/unix"

	"github.com/alsotoes/momo/src/common"
)

type VolumeFile struct {
	file     *os.File
	path     string
	volumeID uint32
	size     int64
	sealed   bool
	mu       sync.RWMutex
}

type VolumeMetadata struct {
	VolumeID    uint32 `json:"volume_id"`
	Size        int64  `json:"size"`
	Sealed      bool   `json:"sealed"`
	CreatedAt   int64  `json:"created_at"`
	SealedAt    int64  `json:"sealed_at,omitempty"`
	NeedleCount int    `json:"needle_count"`
	CommittedAt int64  `json:"committed_at"`
}

type NeedleMetadata struct {
	Hash     []byte `json:"hash"`
	VolumeID uint32 `json:"volume_id"`
	Offset   int64  `json:"offset"`
	Size     int64  `json:"size"`
}

type VolumeBlobStore struct {
	mu sync.RWMutex

	baseDir       string
	activeVolume  *VolumeFile
	volumeSize    int64
	sealedVolumes map[uint32]*VolumeFile
	blobs         *bbolt.DB

	volumeIDCounter uint32
}

// NewVolumeBlobStore creates a VolumeBlobStore rooted at baseDir. Needle
// location metadata is kept in a dedicated <baseDir>/needles.db bbolt file so
// the store never shares a bbolt lock with the CAS metadata DB (momo.db) —
// two bbolt.Open calls on one file deadlock (see #1128).
func NewVolumeBlobStore(baseDir string, volumeSize int64) (*VolumeBlobStore, error) {
	if volumeSize <= 0 {
		volumeSize = common.DefaultVolumeSizeBytes
	}
	if volumeSize > common.MaxVolumeSizeBytes {
		return nil, fmt.Errorf("volume size %d exceeds maximum %d: %w", volumeSize, common.MaxVolumeSizeBytes, syscall.EINVAL)
	}

	if err := os.MkdirAll(baseDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create volume dir: %w", err)
	}

	metaDB, err := bbolt.Open(filepath.Join(baseDir, "needles.db"), 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open needles metadata DB: %w", err)
	}

	vbs := &VolumeBlobStore{
		baseDir:       baseDir,
		volumeSize:    volumeSize,
		sealedVolumes: make(map[uint32]*VolumeFile),
		blobs:         metaDB,
	}

	if err := vbs.initVolumes(); err != nil {
		metaDB.Close()
		return nil, err
	}

	return vbs, nil
}

// volumeIDFromName extracts the numeric ID from a "vol-NNNN.dat" file name,
// returning 0 for anything that is not a volume file.
func volumeIDFromName(name string) uint32 {
	if !strings.HasPrefix(name, VolumeFilePrefix) || !strings.HasSuffix(name, VolumeFileExt) {
		return 0
	}
	idStr := strings.TrimSuffix(strings.TrimPrefix(name, VolumeFilePrefix), VolumeFileExt)
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(id)
}

// volumePath renders the on-disk path for volume id.
func (vbs *VolumeBlobStore) volumePath(id uint32) string {
	return filepath.Join(vbs.baseDir, fmt.Sprintf("%s%04d%s", VolumeFilePrefix, id, VolumeFileExt))
}

// openExistingVolume reopens volume id read-write, or read-only when the
// volume is sealed. Sealing is a persisted metadata state (the file is closed
// for writing as soon as the next needle would not fit, well before the file
// reaches its size cap), so the flag comes from the volumes bucket; the size
// cap is only a fallback for volumes with no metadata record.
func (vbs *VolumeBlobStore) openExistingVolume(id uint32) (*VolumeFile, error) {
	path := vbs.volumePath(id)
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	sealed := fi.Size() >= vbs.volumeSize
	if meta, merr := vbs.getVolumeMetadata(id); merr == nil {
		sealed = meta.Sealed
	}
	mode := os.O_RDWR
	if sealed {
		mode = os.O_RDONLY
	}
	f, err := os.OpenFile(path, mode, 0640)
	if err != nil {
		return nil, err
	}
	return &VolumeFile{
		file:     f,
		path:     path,
		volumeID: id,
		size:     fi.Size(),
		sealed:   sealed,
	}, nil
}

// restoreVolumes reopens persisted volume files, classifying full volumes as
// sealed (read-only) and electing the last under-sized one as active. It
// creates a fresh active volume when none survived the restart.
func (vbs *VolumeBlobStore) restoreVolumes() error {
	entries, err := os.ReadDir(vbs.baseDir)
	if err != nil {
		return fmt.Errorf("failed to read base dir: %w", err)
	}

	var maxID uint32
	for _, e := range entries {
		if id := volumeIDFromName(e.Name()); id > maxID {
			maxID = id
		}
	}
	vbs.volumeIDCounter = maxID

	for id := uint32(1); id <= maxID; id++ {
		vf, err := vbs.openExistingVolume(id)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				log.Printf("WARN: failed to open volume %s: %v", vbs.volumePath(id), err)
			}
			continue
		}
		if vf.sealed {
			vbs.sealedVolumes[vf.volumeID] = vf
		} else {
			vbs.activeVolume = vf
		}
	}

	if vbs.activeVolume == nil {
		return vbs.createNewVolume()
	}
	return nil
}

// initVolumes prepares the volume directory and restores persisted volumes.
func (vbs *VolumeBlobStore) initVolumes() error {
	if err := os.MkdirAll(vbs.baseDir, 0750); err != nil {
		return fmt.Errorf("failed to create base dir: %w", err)
	}
	return vbs.restoreVolumes()
}

// createNewVolume opens the next volume and installs it as the active one.
func (vbs *VolumeBlobStore) createNewVolume() error {
	vbs.mu.Lock()
	defer vbs.mu.Unlock()
	return vbs.createNewVolumeLocked()
}

// createNewVolumeLocked is createNewVolume with vbs.mu already held. Keeping
// the unlocked form lets sealCurrentVolume roll volumes without re-entering
// the (non-reentrant) store mutex.
func (vbs *VolumeBlobStore) createNewVolumeLocked() error {
	newID := atomic.AddUint32(&vbs.volumeIDCounter, 1)
	path := filepath.Join(vbs.baseDir, fmt.Sprintf("%s%04d%s", VolumeFilePrefix, newID, VolumeFileExt))

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0640)
	if err != nil {
		return fmt.Errorf("failed to create volume %s: %w", path, err)
	}

	vbs.activeVolume = &VolumeFile{
		file:     f,
		path:     path,
		volumeID: newID,
		size:     0,
		sealed:   false,
	}

	if vbs.blobs != nil {
		meta := VolumeMetadata{
			VolumeID:  newID,
			Size:      0,
			Sealed:    false,
			CreatedAt: time.Now().UnixNano(),
		}
		if err := vbs.saveVolumeMetadata(newID, meta); err != nil {
			log.Printf("WARN: failed to persist volume metadata for %d: %v", newID, err)
		}
	}

	log.Printf("Created new volume %s (ID: %d)", path, newID)
	return nil
}

// decodeContentHash validates an object hash and returns its 32 decoded bytes.
func decodeContentHash(hash string) ([32]byte, error) {
	var out [32]byte
	if common.HasPathTraversalChars(hash) {
		return out, fmt.Errorf("invalid hash contains path traversal characters: %w", syscall.EINVAL)
	}
	raw, err := hex.DecodeString(hash)
	if err != nil {
		return out, fmt.Errorf("invalid hash: %w", err)
	}
	if len(raw) != 32 {
		return out, fmt.Errorf("invalid hash length: expected 32 bytes, got %d", len(raw))
	}
	copy(out[:], raw)
	return out, nil
}

// readVerifiedPayload reads the whole content stream and confirms it hashes to
// the given content hash — the CAS write path's integrity gate.
func readVerifiedPayload(hashBytes [32]byte, content io.Reader) ([]byte, error) {
	payload, err := io.ReadAll(content)
	if err != nil {
		return nil, fmt.Errorf("failed to read payload: %w", err)
	}
	sum := sha256.Sum256(payload)
	if !bytesEqual(hashBytes[:], sum[:]) {
		return nil, fmt.Errorf("hash mismatch: expected %x, got %x", hashBytes, sum)
	}
	return payload, nil
}

// needleCRC computes the CRC32C over the full needle frame
// (Magic || Length || Hash || Flags || Payload), matching readNeedle.
func needleCRC(hashBytes [32]byte, payload []byte) uint32 {
	frame := make([]byte, NeedleHeaderSize+len(payload))
	binary.BigEndian.PutUint32(frame[0:4], uint32(NeedleMagic))
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload)))
	copy(frame[8:40], hashBytes[:])
	binary.BigEndian.PutUint16(frame[40:42], uint16(NeedleFlagNone))
	copy(frame[42:42+len(payload)], payload)
	return crc32.Checksum(frame, crc32.MakeTable(crc32.Castagnoli))
}

// reserveNeedleSlot returns the active volume plus the write offset, rolling
// to a fresh volume first when the needle would not fit. It returns
// syscall.ENOSPC when even a freshly sealed successor cannot fit the needle.
func (vbs *VolumeBlobStore) reserveNeedleSlot(needleSize int64) (*VolumeFile, int64, error) {
	vbs.mu.RLock()
	vf := vbs.activeVolume
	vbs.mu.RUnlock()
	if vf == nil {
		return nil, 0, fmt.Errorf("no active volume available")
	}

	vf.mu.Lock()
	fits := vf.size+needleSize <= vbs.volumeSize
	vf.mu.Unlock()
	if fits {
		return vf, vf.size, nil
	}

	if err := vbs.sealCurrentVolume(); err != nil {
		return nil, 0, err
	}
	vbs.mu.RLock()
	vf = vbs.activeVolume
	vbs.mu.RUnlock()
	if vf == nil {
		return nil, 0, fmt.Errorf("no active volume available after seal")
	}
	vf.mu.Lock()
	defer vf.mu.Unlock()
	if vf.size+needleSize > vbs.volumeSize {
		return nil, 0, fmt.Errorf("volume full after seal: %w", syscall.ENOSPC)
	}
	return vf, vf.size, nil
}

// appendNeedle writes the frame bytes at the current file offset and syncs.
// The caller holds vf.mu and owns the tail via vf.size. Frame layout and
// checksumming are delegated to writeNeedle so there is exactly one writer.
func (vf *VolumeFile) appendNeedle(hashBytes [32]byte, payload []byte) error {
	if _, err := writeNeedle(vf.file, hashBytes, payload, NeedleFlagNone); err != nil {
		return fmt.Errorf("failed to append needle: %w", err)
	}
	if err := vf.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync volume: %w", err)
	}
	return nil
}

// PutBlob appends a needle carrying the verified payload to the active volume,
// sealing the volume first when the needle would not fit.
func (vbs *VolumeBlobStore) PutBlob(hash string, content io.Reader) (err error) {
	defer common.RecoverErr("VolumeBlobStore.PutBlob", &err)

	hashBytes, err := decodeContentHash(hash)
	if err != nil {
		return err
	}

	payload, err := readVerifiedPayload(hashBytes, content)
	if err != nil {
		return err
	}

	needleSize := int64(NeedleOverhead) + int64(alignTo8(len(payload)))
	vf, offset, err := vbs.reserveNeedleSlot(needleSize)
	if err != nil {
		return err
	}

	vf.mu.Lock()
	defer vf.mu.Unlock()
	if err := vf.appendNeedle(hashBytes, payload); err != nil {
		return err
	}

	vf.size += needleSize

	if vbs.blobs != nil {
		if err = vbs.saveNeedleMetadata(hash, vf.volumeID, offset, len(payload)); err != nil {
			log.Printf("WARN: failed to save needle metadata: %v", err)
		}
	}
	return nil
}

func (vbs *VolumeBlobStore) GetBlob(hash string) (rc io.ReadCloser, err error) {
	defer common.RecoverErr("VolumeBlobStore.GetBlob", &err)

	if _, derr := decodeContentHash(hash); derr != nil {
		return nil, derr
	}

	meta, err := vbs.getNeedleMetadata(hash)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil, syscall.ENOENT
		}
		return nil, err
	}

	vbs.mu.RLock()
	var vf *VolumeFile
	if meta.VolumeID == vbs.activeVolume.volumeID {
		vf = vbs.activeVolume
	} else {
		vf = vbs.sealedVolumes[meta.VolumeID]
	}
	vbs.mu.RUnlock()

	if vf == nil {
		return nil, fmt.Errorf("volume %d not found", meta.VolumeID)
	}

	vf.mu.RLock()
	defer vf.mu.RUnlock()

	if vf.file == nil {
		return nil, fmt.Errorf("volume file not open")
	}

	needle, err := readNeedle(vf.file, meta.Offset)
	if err != nil {
		return nil, fmt.Errorf("failed to read needle: %w", err)
	}

	if !bytesEqual(meta.Hash, needle.Header.Hash[:]) {
		return nil, fmt.Errorf("hash mismatch in volume")
	}

	return io.NopCloser(bytes.NewReader(needle.Payload)), nil
}

func (vbs *VolumeBlobStore) DeleteBlob(hash string) (err error) {
	defer common.RecoverErr("VolumeBlobStore.DeleteBlob", &err)

	if _, derr := decodeContentHash(hash); derr != nil {
		return derr
	}

	meta, err := vbs.getNeedleMetadata(hash)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return syscall.ENOENT
		}
		return err
	}

	vbs.mu.RLock()
	var vf *VolumeFile
	if meta.VolumeID == vbs.activeVolume.volumeID {
		vf = vbs.activeVolume
	} else {
		vf = vbs.sealedVolumes[meta.VolumeID]
	}
	vbs.mu.RUnlock()

	if vf == nil {
		return fmt.Errorf("volume %d not found", meta.VolumeID)
	}

	if vf.sealed {
		return fmt.Errorf("cannot delete from sealed volume: %w", syscall.EPERM)
	}

	vf.mu.Lock()
	defer vf.mu.Unlock()

	err = unix.Fallocate(int(vf.file.Fd()), unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE,
		meta.Offset, int64(NeedleOverhead)+int64(alignTo8(int(meta.Size))))
	if err != nil {
		log.Printf("WARN: hole punching failed, falling back to tombstone: %v", err)
		return vbs.writeTombstone(vf, hash)
	}

	if vbs.blobs != nil {
		if err = vbs.deleteNeedleMetadata(hash); err != nil {
			log.Printf("WARN: failed to delete needle metadata: %v", err)
		}
	}

	return nil
}

func (vbs *VolumeBlobStore) writeTombstone(vf *VolumeFile, hash string) error {
	return nil
}

func (vbs *VolumeBlobStore) sealCurrentVolume() error {
	vbs.mu.Lock()
	defer vbs.mu.Unlock()

	if vbs.activeVolume == nil || vbs.activeVolume.sealed {
		return nil
	}

	vf := vbs.activeVolume
	vf.mu.Lock()
	if err := vf.file.Sync(); err != nil {
		vf.mu.Unlock()
		return fmt.Errorf("failed to sync volume before sealing: %w", err)
	}
	vf.sealed = true
	vf.mu.Unlock()

	vbs.sealedVolumes[vf.volumeID] = vf
	vbs.activeVolume = nil

	if vbs.blobs != nil {
		if meta, err := vbs.getVolumeMetadata(vf.volumeID); err == nil {
			meta.Sealed = true
			meta.SealedAt = time.Now().UnixNano()
			vbs.saveVolumeMetadata(vf.volumeID, meta)
		}
	}

	return vbs.createNewVolumeLocked()
}

func (vbs *VolumeBlobStore) Close() error {
	vbs.mu.Lock()
	defer vbs.mu.Unlock()

	if vbs.blobs != nil {
		if err := vbs.blobs.Close(); err != nil {
			log.Printf("WARN: failed to close needles metadata DB: %v", err)
		}
		vbs.blobs = nil
	}

	if vbs.activeVolume != nil {
		vbs.activeVolume.mu.Lock()
		if vbs.activeVolume.file != nil {
			vbs.activeVolume.file.Close()
		}
		vbs.activeVolume.mu.Unlock()
	}

	for _, vf := range vbs.sealedVolumes {
		vf.mu.Lock()
		if vf.file != nil {
			vf.file.Close()
		}
		vf.mu.Unlock()
	}

	return nil
}

func (vbs *VolumeBlobStore) saveVolumeMetadata(volumeID uint32, meta VolumeMetadata) error {
	if vbs.blobs == nil {
		return nil
	}
	return vbs.blobs.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("volumes"))
		if err != nil {
			return err
		}
		data, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		key := make([]byte, 4)
		binary.BigEndian.PutUint32(key, volumeID)
		return b.Put(key, data)
	})
}

func (vbs *VolumeBlobStore) getVolumeMetadata(volumeID uint32) (VolumeMetadata, error) {
	var meta VolumeMetadata
	err := vbs.blobs.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("volumes"))
		if b == nil {
			return syscall.ENOENT
		}
		key := make([]byte, 4)
		binary.BigEndian.PutUint32(key, volumeID)
		data := b.Get(key)
		if data == nil {
			return syscall.ENOENT
		}
		return json.Unmarshal(data, &meta)
	})
	if err != nil {
		return VolumeMetadata{}, err
	}
	return meta, nil
}

func (vbs *VolumeBlobStore) saveNeedleMetadata(hash string, volumeID uint32, offset int64, size int) error {
	if vbs.blobs == nil {
		return nil
	}
	return vbs.blobs.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("needles"))
		if err != nil {
			return err
		}
		decoded, derr := hex.DecodeString(hash)
		if derr != nil {
			return derr
		}
		meta := NeedleMetadata{
			Hash:     decoded,
			VolumeID: volumeID,
			Offset:   offset,
			Size:     int64(alignTo8(size)),
		}
		data, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		return b.Put([]byte(hash), data)
	})
}

func (vbs *VolumeBlobStore) getNeedleMetadata(hash string) (NeedleMetadata, error) {
	var meta NeedleMetadata
	err := vbs.blobs.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("needles"))
		if b == nil {
			return syscall.ENOENT
		}
		data := b.Get([]byte(hash))
		if data == nil {
			return syscall.ENOENT
		}
		return json.Unmarshal(data, &meta)
	})
	if err != nil {
		return NeedleMetadata{}, err
	}
	return meta, nil
}

func (vbs *VolumeBlobStore) deleteNeedleMetadata(hash string) error {
	if vbs.blobs == nil {
		return nil
	}
	return vbs.blobs.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("needles"))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(hash))
	})
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
