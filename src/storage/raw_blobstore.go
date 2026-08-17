package storage

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/alsotoes/momo/src/common"
	"go.etcd.io/bbolt"
)

var bucketRawAlloc = []byte("raw_alloc")

// RawBlobStore implements BlobStore using direct I/O on a raw block device.
// A bump allocator assigns each blob a contiguous region. The allocation
// table (hash → offset+length) is stored in a local bbolt DB.
// Deleted blob space is not reclaimed (fragmentation); a compaction
// pass can be added in a future enhancement.
type RawBlobStore struct {
	device     *os.File
	allocDB    *bbolt.DB
	mu         sync.Mutex
	nextOffset int64
}

// NewRawBlobStore creates a new RawBlobStore. The device path is taken
// from cfg.RawDevicePath, falling back to daemon.Drive. The allocation
// table DB is stored in daemon.Data/raw_alloc.db.
func NewRawBlobStore(cfg common.ConfigurationStorage, daemon *common.Daemon) (rbs *RawBlobStore, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in NewRawBlobStore: %v", r)
			err = fmt.Errorf("raw: initialization panic: %v: %w", r, syscall.EIO)
		}
	}()

	devicePath := cfg.RawDevicePath
	if devicePath == "" {
		devicePath = daemon.Drive
	}
	if devicePath == "" {
		return nil, fmt.Errorf("raw device path is required (set raw_device_path or daemon.drive): %w", syscall.EINVAL)
	}

	if err := os.MkdirAll(daemon.Data, 0755); err != nil {
		return nil, fmt.Errorf("raw: failed to create data dir: %w", syscall.EIO)
	}

	if dir := filepath.Dir(devicePath); dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("raw: failed to create device dir: %w", syscall.EIO)
		}
	}

	device, err := os.OpenFile(devicePath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("raw: failed to open device %s: %w", devicePath, syscall.EIO)
	}

	allocPath := filepath.Join(daemon.Data, "raw_alloc.db")
	allocDB, err := bbolt.Open(allocPath, 0600, nil)
	if err != nil {
		device.Close()
		return nil, fmt.Errorf("raw: failed to open allocation DB: %w", syscall.EIO)
	}

	var nextOffset int64
	err = allocDB.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketRawAlloc)
		if err != nil {
			return err
		}
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if len(v) == 16 {
				offset := int64(binary.BigEndian.Uint64(v[0:8]))
				length := int64(binary.BigEndian.Uint64(v[8:16]))
				if offset > 0 && length > 0 && offset <= math.MaxInt64-length {
					end := offset + length
					if end > nextOffset {
						nextOffset = end
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		allocDB.Close()
		device.Close()
		return nil, fmt.Errorf("raw: failed to init allocation table: %w", err)
	}

	return &RawBlobStore{
		device:     device,
		allocDB:    allocDB,
		nextOffset: nextOffset,
	}, nil
}

func (r *RawBlobStore) Close() error {
	if err := r.allocDB.Close(); err != nil {
		log.Printf("AUDIT: allocDB.Close() failed: %v", err)
	}
	return r.device.Close()
}

// PutBlob writes a blob to the raw device at the next available offset.
// If the hash already exists in the allocation table, it is a no-op.
// Content is streamed in 64 KB chunks to avoid unbounded memory allocation.
// The total size is capped at common.MaxFileSize (Rule 4 — Zero-Crash Pattern).
func (r *RawBlobStore) PutBlob(hash string, content io.Reader) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("CRITICAL: Panic recovered in RawBlobStore.PutBlob: %v", rec)
			err = fmt.Errorf("raw: write panic: %v: %w", rec, syscall.EIO)
		}
	}()

	if common.HasPathTraversalChars(hash) {
		return fmt.Errorf("raw: invalid hash contains path traversal characters: %w", syscall.EINVAL)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var exists bool
	err = r.allocDB.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketRawAlloc)
		if b != nil {
			exists = b.Get([]byte(hash)) != nil
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("raw: failed to check existence: %w", err)
	}
	if exists {
		return nil
	}

	limited := io.LimitReader(content, common.MaxFileSize+1)
	offset := r.nextOffset

	if offset > math.MaxInt64-common.MaxFileSize {
		return fmt.Errorf("raw: offset %d would overflow with MaxFileSize %d: %w", offset, common.MaxFileSize, syscall.EIO)
	}

	buf := make([]byte, 64*1024)
	var totalWritten int64

	for {
		n, readErr := limited.Read(buf)
		if n > 0 {
			written, writeErr := r.device.WriteAt(buf[:n], offset+totalWritten)
			if writeErr != nil {
				return fmt.Errorf("raw: failed to write to device: %w", syscall.EIO)
			}
			if written != n {
				return fmt.Errorf("raw: short write: %w", syscall.EIO)
			}
			totalWritten += int64(written)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("raw: failed to read content: %w", syscall.EIO)
		}
	}

	if totalWritten > common.MaxFileSize {
		return fmt.Errorf("raw: blob exceeds MaxFileSize (%d bytes): %w", common.MaxFileSize, syscall.EFBIG)
	}

	if offset > math.MaxInt64-totalWritten {
		return fmt.Errorf("raw: offset overflow: %d + %d exceeds MaxInt64: %w", offset, totalWritten, syscall.EIO)
	}

	var alloc [16]byte
	binary.BigEndian.PutUint64(alloc[0:8], uint64(offset))
	binary.BigEndian.PutUint64(alloc[8:16], uint64(totalWritten))
	err = r.allocDB.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketRawAlloc).Put([]byte(hash), alloc[:])
	})
	if err != nil {
		return fmt.Errorf("raw: failed to record allocation: %w", syscall.EIO)
	}

	r.nextOffset = offset + totalWritten
	return nil
}

// GetBlob reads a blob from the raw device by its content hash.
func (r *RawBlobStore) GetBlob(hash string) (rc io.ReadCloser, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("CRITICAL: Panic recovered in RawBlobStore.GetBlob: %v", rec)
			err = fmt.Errorf("raw: read panic: %v: %w", rec, syscall.EIO)
		}
	}()

	if common.HasPathTraversalChars(hash) {
		return nil, fmt.Errorf("raw: invalid hash contains path traversal characters: %w", syscall.EINVAL)
	}

	var offset, length int64
	var found bool
	err = r.allocDB.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketRawAlloc)
		if b == nil {
			return nil
		}
		v := b.Get([]byte(hash))
		if v != nil && len(v) == 16 {
			offset = int64(binary.BigEndian.Uint64(v[0:8]))
			length = int64(binary.BigEndian.Uint64(v[8:16]))
			found = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, syscall.ENOENT
	}

	if length <= 0 || length > common.MaxFileSize {
		return nil, fmt.Errorf("raw: invalid blob length %d in alloc table: %w", length, syscall.EIO)
	}
	if offset < 0 {
		return nil, fmt.Errorf("raw: invalid blob offset %d in alloc table: %w", offset, syscall.EIO)
	}

	// Stream directly from the device — no full-blob allocation.
	return io.NopCloser(io.NewSectionReader(r.device, offset, length)), nil
}

// DeleteBlob removes a blob's allocation entry. The device space is not
// reclaimed (bump allocator; compaction is a future enhancement).
// Missing blobs are silently ignored.
func (r *RawBlobStore) DeleteBlob(hash string) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("CRITICAL: Panic recovered in RawBlobStore.DeleteBlob: %v", rec)
			err = fmt.Errorf("raw: delete panic: %v: %w", rec, syscall.EIO)
		}
	}()

	if common.HasPathTraversalChars(hash) {
		return fmt.Errorf("raw: invalid hash contains path traversal characters: %w", syscall.EINVAL)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	err = r.allocDB.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketRawAlloc)
		if b == nil {
			return nil
		}
		return b.Delete([]byte(hash))
	})
	if err != nil {
		return fmt.Errorf("raw: failed to delete allocation: %w", syscall.EIO)
	}
	return nil
}

var _ BlobStore = (*RawBlobStore)(nil)
