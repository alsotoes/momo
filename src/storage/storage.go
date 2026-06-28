package storage

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"github.com/alsotoes/momo/src/common"
	"go.etcd.io/bbolt"
)

// Buckets used in Bbolt
var (
	bucketObjects   = []byte("objects")   // Maps ContentHash -> {Metadata JSON}
	bucketNamespace = []byte("namespace") // Maps FileName -> ContentHash
	bucketPaths     = []byte("paths")     // Maps FileName -> RemotePath
)

// Store defines the interface for object storage operations.
type Store interface {
	io.Closer
	Put(name string, hash string, size int64, remotePath string, content io.Reader) error
	Get(name string) (io.ReadCloser, common.FileMetadata, error)
	Has(hash string) (bool, error)
	Delete(name string) error
	GetBlobPath(name string) (string, error)
}

// CASStore implements Content-Addressable Storage with Bbolt metadata.
type CASStore struct {
	mu     sync.RWMutex
	db     *bbolt.DB
	base   string
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
		_, err := tx.CreateBucketIfNotExists(bucketPaths)
		return err
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	return &CASStore{
		db:   db,
		base: dataDir,
	}, nil
}

func (s *CASStore) Close() error {
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
		// ⚡ Bolt: Store size as a simple 8-byte binary value for speed.
		// In a full implementation, we would store a JSON struct with RefCount.
		// ⚡ Bolt: Eliminate heap allocation and GC overhead for number to byte slice conversion
		var sizeBuf [32]byte
		if err := obj.Put([]byte(hash), strconv.AppendInt(sizeBuf[:0], size, 10)); err != nil {
			return fmt.Errorf("metadata error: %w", syscall.EIO)
		}

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
		// 🛡️ Zero-Crash: Guard against missing metadata to prevent nil pointer dereference in unsafe.SliceData.
		if val == nil {
			return fmt.Errorf("metadata missing for hash %s: %w", hash, syscall.ENOENT)
		}

		// ⚡ Bolt: Eliminate allocs and overhead of fmt.Sscanf by using strconv.ParseInt with unsafe.String.
		var parseErr error
		size, parseErr = strconv.ParseInt(unsafe.String(unsafe.SliceData(val), len(val)), 10, 64)
		if parseErr != nil {
			return fmt.Errorf("metadata corruption for hash %s: %w", hash, syscall.EBADMSG)
		}

		// 🛡️ Zero-Crash: Ensure size is non-negative to prevent downstream overflows or invalid allocations.
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

	// Simple deletion of the namespace entry. 
	// Real CAS would implement reference counting and garbage collection for the blobs.
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketNamespace).Delete([]byte(name))
	})
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
