package storage

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"go.etcd.io/bbolt"
)

// Buckets used in Bbolt
var (
	bucketObjects    = []byte("objects")    // Maps ContentHash -> {ObjectMeta binary}
	bucketNamespace  = []byte("namespace")  // Maps FileName -> ContentHash
	bucketPaths      = []byte("paths")      // Maps FileName -> RemotePath
	bucketTombstones = []byte("tombstones") // Maps FileName -> deletion timestamp (unix nano)
)

// ObjectMeta is the binary metadata stored in the objects bucket.
// Wire format: [8B size (big-endian)] [8B refCount (big-endian)] [8B deletedAt (big-endian)]
type ObjectMeta struct {
	Size      int64
	RefCount  int64
	DeletedAt int64 // unix nano; 0 = not deleted
}

// encodeObjectMeta serializes ObjectMeta into a fixed 24-byte binary slice.
func (m ObjectMeta) encode() []byte {
	buf := make([]byte, 24)
	binary.BigEndian.PutUint64(buf[0:8], uint64(m.Size))
	binary.BigEndian.PutUint64(buf[8:16], uint64(m.RefCount))
	binary.BigEndian.PutUint64(buf[16:24], uint64(m.DeletedAt))
	return buf
}

// decodeObjectMeta deserializes a 24-byte binary slice into ObjectMeta.
// Falls back to legacy ASCII size format for backward compatibility.
func decodeObjectMeta(val []byte) ObjectMeta {
	if len(val) == 24 {
		return ObjectMeta{
			Size:      int64(binary.BigEndian.Uint64(val[0:8])),
			RefCount:  int64(binary.BigEndian.Uint64(val[8:16])),
			DeletedAt: int64(binary.BigEndian.Uint64(val[16:24])),
		}
	}
	// Legacy format: ASCII integer = size only, refCount=1, not deleted
	size, _ := strconv.ParseInt(string(val), 10, 64)
	return ObjectMeta{Size: size, RefCount: 1}
}

// Store defines the interface for object storage operations.
type Store interface {
	io.Closer
	Put(name string, hash string, size int64, remotePath string, content io.Reader) error
	Get(name string) (io.ReadCloser, common.FileMetadata, error)
	Has(hash string) (bool, error)
	Delete(name string) error
	GetBlobPath(name string) (string, error)
	List() ([]common.FileMetadata, error)
}

// CASStore implements Content-Addressable Storage with Bbolt metadata.
type CASStore struct {
	mu     sync.RWMutex
	db     *bbolt.DB
	base   string
	gcDone chan struct{}
	gcWG   sync.WaitGroup
}

// NewCASStore initializes a new CAS storage backend.
func NewCASStore(dataDir string) (*CASStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data dir: %w", syscall.EIO)
	}

	dbPath := filepath.Join(dataDir, "momo.db")
	db, err := bbolt.Open(dbPath, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open bbolt: %w", syscall.EIO)
	}

	// Initialize buckets
	err = db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketObjects); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketNamespace); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketPaths); err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists(bucketTombstones)
		return err
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	return &CASStore{
		db:     db,
		base:   dataDir,
		gcDone: make(chan struct{}),
	}, nil
}

func (s *CASStore) Close() error {
	if s.gcDone != nil {
		close(s.gcDone)
		s.gcWG.Wait()
	}
	return s.db.Close()
}

// Put saves an object to the store.
// If the hash already exists, it only updates the namespace mapping (deduplication).
func (s *CASStore) Put(name string, hash string, size int64, remotePath string, content io.Reader) (err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics in the storage backend.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in CASStore.Put for %s: %v", name, r)
			err = fmt.Errorf("internal storage panic: %w", syscall.EIO)
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Check if we already have the blob
	exists, _ := s.hasInternal(hash)
	if !exists && content != nil {
		// 🛡️ Zero-Crash: Use atomic rename to ensure data integrity.
		blobPath := s.getBlobPath(hash)
		if err := os.MkdirAll(filepath.Dir(blobPath), 0755); err != nil {
			return fmt.Errorf("storage error: failed to create tiered dir: %w", syscall.EIO)
		}

		tmpFile, err := os.CreateTemp(s.base, "blob-*.tmp")
		if err != nil {
			return fmt.Errorf("storage error: failed to create temp file: %w", syscall.EIO)
		}
		tmpPath := tmpFile.Name()

		// ⚡ Bolt: Use a buffered writer to optimize disk I/O and minimize syscalls.
		writer := bufio.NewWriterSize(tmpFile, 64*1024) // 64KB buffer
		if _, err := io.Copy(writer, content); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("storage error: failed to write blob: %w", syscall.ENOSPC)
		}

		if err := writer.Flush(); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("storage error: failed to flush blob: %w", syscall.EIO)
		}
		tmpFile.Close()

		if err := os.Rename(tmpPath, blobPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("storage error: failed to commit blob: %w", syscall.EIO)
		}
	}

	// 2. Update Metadata
	return s.db.Update(func(tx *bbolt.Tx) error {
		ns := tx.Bucket(bucketNamespace)
		if err := ns.Put([]byte(name), []byte(hash)); err != nil {
			return fmt.Errorf("metadata error: %w", syscall.EIO)
		}

		obj := tx.Bucket(bucketObjects)
		// Update or create object metadata with reference counting.
		var meta ObjectMeta
		if existing := obj.Get([]byte(hash)); existing != nil {
			meta = decodeObjectMeta(existing)
			meta.RefCount++
		} else {
			meta = ObjectMeta{Size: size, RefCount: 1}
		}
		if err := obj.Put([]byte(hash), meta.encode()); err != nil {
			return fmt.Errorf("metadata error: %w", syscall.EIO)
		}

		// Remove any existing tombstone for this name (resurrection).
		tx.Bucket(bucketTombstones).Delete([]byte(name))

		// Store RemotePath
		if remotePath != "" {
			normalized, err := common.NormalizeVirtualPath(remotePath)
			if err != nil {
				return fmt.Errorf("invalid virtual path %q: %w", remotePath, err)
			}
			if len(normalized)+1+len(name) > common.FileInfoLength {
				return fmt.Errorf("virtual path and name concatenation too long: %w", syscall.ENAMETOOLONG)
			}
			paths := tx.Bucket(bucketPaths)
			if err := paths.Put([]byte(name), []byte(normalized)); err != nil {
				return fmt.Errorf("metadata error: %w", syscall.EIO)
			}
		}
		return nil
	})
}

// Get retrieves an object by its human-readable name.
func (s *CASStore) Get(name string) (rc io.ReadCloser, meta common.FileMetadata, err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics during metadata parsing.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in CASStore.Get for %s: %v", name, r)
			err = fmt.Errorf("internal storage panic: %w", syscall.EIO)
		}
	}()

	s.mu.RLock()
	defer s.mu.RUnlock()

	var hash string
	err = s.db.View(func(tx *bbolt.Tx) error {
		// Check tombstone first — deleted names should appear as not found.
		if ts := tx.Bucket(bucketTombstones).Get([]byte(name)); ts != nil {
			return syscall.ENOENT
		}
		h := tx.Bucket(bucketNamespace).Get([]byte(name))
		if h == nil {
			return syscall.ENOENT
		}
		hash = string(h)
		return nil
	})
	if err != nil {
		return nil, common.FileMetadata{}, err
	}

	blobPath := s.getBlobPath(hash)
	f, openErr := os.Open(blobPath)
	if openErr != nil {
		return nil, common.FileMetadata{}, openErr
	}

	// 🛡️ Zero-Crash: Ensure file is closed if subsequent metadata lookups fail.
	defer func() {
		if err != nil {
			f.Close()
		}
	}()

	// Read metadata from DB
	var size int64
	err = s.db.View(func(tx *bbolt.Tx) error {
		val := tx.Bucket(bucketObjects).Get([]byte(hash))
		if val == nil {
			return fmt.Errorf("metadata missing for hash %s: %w", hash, syscall.ENOENT)
		}

		meta := decodeObjectMeta(val)
		size = meta.Size

		if size < 0 {
			return fmt.Errorf("invalid size %d for hash %s: %w", size, hash, syscall.EBADMSG)
		}
		return nil
	})
	if err != nil {
		return nil, common.FileMetadata{}, err
	}

	var remotePath string
	_ = s.db.View(func(tx *bbolt.Tx) error {
		p := tx.Bucket(bucketPaths).Get([]byte(name))
		if p != nil {
			remotePath = string(p)
		}
		return nil
	})

	return f, common.FileMetadata{Name: name, Hash: hash, Size: size, RemotePath: remotePath}, nil
}

// Has checks if a content hash exists in the store.
func (s *CASStore) Has(hash string) (exists bool, err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in CASStore.Has for %s: %v", hash, r)
			err = fmt.Errorf("internal storage panic: %w", syscall.EIO)
		}
	}()

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hasInternal(hash)
}

func (s *CASStore) hasInternal(hash string) (bool, error) {
	var exists bool
	err := s.db.View(func(tx *bbolt.Tx) error {
		val := tx.Bucket(bucketObjects).Get([]byte(hash))
		exists = (val != nil)
		return nil
	})
	return exists, err
}

func (s *CASStore) Delete(name string) (err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in CASStore.Delete for %s: %v", name, r)
			err = fmt.Errorf("internal storage panic: %w", syscall.EIO)
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	return s.db.Update(func(tx *bbolt.Tx) error {
		ns := tx.Bucket(bucketNamespace)
		obj := tx.Bucket(bucketObjects)
		paths := tx.Bucket(bucketPaths)
		ts := tx.Bucket(bucketTombstones)

		// Write tombstone (8-byte unix nano timestamp).
		var tsBuf [8]byte
		binary.BigEndian.PutUint64(tsBuf[:], uint64(now))
		if err := ts.Put([]byte(name), tsBuf[:]); err != nil {
			return fmt.Errorf("metadata error: %w", syscall.EIO)
		}

		// Look up the hash for this name to decrement refcount.
		h := ns.Get([]byte(name))
		if h != nil {
			hash := string(h)
			if val := obj.Get([]byte(hash)); val != nil {
				meta := decodeObjectMeta(val)
				meta.RefCount--
				if meta.RefCount <= 0 {
					meta.RefCount = 0
					meta.DeletedAt = now
				}
				if err := obj.Put([]byte(hash), meta.encode()); err != nil {
					return fmt.Errorf("metadata error: %w", syscall.EIO)
				}
			}
		}

		// Remove namespace and paths entries.
		ns.Delete([]byte(name))
		paths.Delete([]byte(name))
		return nil
	})
}

// List retrieves all file metadata entries in the store.
func (s *CASStore) List() (list []common.FileMetadata, err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in CASStore.List: %v", r)
			err = fmt.Errorf("internal storage panic: %w", syscall.EIO)
		}
	}()

	s.mu.RLock()
	defer s.mu.RUnlock()

	err = s.db.View(func(tx *bbolt.Tx) error {
		ns := tx.Bucket(bucketNamespace)
		if ns == nil {
			return nil
		}
		obj := tx.Bucket(bucketObjects)
		paths := tx.Bucket(bucketPaths)
		ts := tx.Bucket(bucketTombstones)

		c := ns.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			name := string(k)
			hash := string(v)

			// Skip tombstoned entries.
			if ts != nil && ts.Get(k) != nil {
				continue
			}

			var size int64 = 0
			var remotePath string = ""

			if obj != nil {
				sizeBytes := obj.Get(v)
				if len(sizeBytes) > 0 {
					size = decodeObjectMeta(sizeBytes).Size
				}
			}

			if paths != nil {
				pBytes := paths.Get(k)
				if pBytes != nil {
					remotePath = string(pBytes)
				}
			}

			list = append(list, common.FileMetadata{
				Name:       name,
				Hash:       hash,
				Size:       size,
				RemotePath: remotePath,
			})
		}
		return nil
	})

	return list, err
}

func (s *CASStore) GetBlobPath(name string) (path string, err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in CASStore.GetBlobPath for %s: %v", name, r)
			err = fmt.Errorf("internal storage panic: %w", syscall.EIO)
		}
	}()

	s.mu.RLock()
	defer s.mu.RUnlock()

	var hash string
	err = s.db.View(func(tx *bbolt.Tx) error {
		if ts := tx.Bucket(bucketTombstones).Get([]byte(name)); ts != nil {
			return syscall.ENOENT
		}
		h := tx.Bucket(bucketNamespace).Get([]byte(name))
		if h == nil {
			return syscall.ENOENT
		}
		hash = string(h)
		return nil
	})
	if err != nil {
		return "", err
	}

	return s.getBlobPath(hash), nil
}

// getBlobPath transforms a hash into a tiered directory path.
// Hash "abcdef123..." -> "data/blobs/ab/cd/ef/abcdef123..."
func (s *CASStore) getBlobPath(hash string) string {
	if len(hash) < 6 {
		return filepath.Join(s.base, "blobs", hash)
	}
	return filepath.Join(s.base, "blobs", hash[0:2], hash[2:4], hash[4:6], hash)
}
