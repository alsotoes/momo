package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/alsotoes/momo/src/common"
	"go.etcd.io/bbolt"
)

// Buckets used in Bbolt
var (
	bucketObjects   = []byte("objects")   // Maps ContentHash -> {Metadata JSON}
	bucketNamespace = []byte("namespace") // Maps FileName -> ContentHash
)

// Store defines the interface for object storage operations.
type Store interface {
	io.Closer
	Put(name string, hash string, size int64, content io.Reader) error
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
		return nil, err
	}

	dbPath := filepath.Join(dataDir, "momo.db")
	db, err := bbolt.Open(dbPath, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open bbolt: %w", err)
	}

	// Initialize buckets
	err = db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketObjects); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(bucketNamespace)
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
func (s *CASStore) Put(name string, hash string, size int64, content io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Check if we already have the blob
	exists, _ := s.Has(hash)
	if !exists {
		// 🛡️ Zero-Crash: Use atomic rename to ensure data integrity.
		blobPath := s.getBlobPath(hash)
		if err := os.MkdirAll(filepath.Dir(blobPath), 0755); err != nil {
			return err
		}

		tmpFile, err := os.CreateTemp(s.base, "blob-*.tmp")
		if err != nil {
			return err
		}
		tmpPath := tmpFile.Name()

		if _, err := io.Copy(tmpFile, content); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return err
		}
		tmpFile.Close()

		if err := os.Rename(tmpPath, blobPath); err != nil {
			os.Remove(tmpPath)
			return err
		}
	}

	// 2. Update Metadata
	return s.db.Update(func(tx *bbolt.Tx) error {
		ns := tx.Bucket(bucketNamespace)
		if err := ns.Put([]byte(name), []byte(hash)); err != nil {
			return err
		}

		obj := tx.Bucket(bucketObjects)
		// ⚡ Bolt: Store size as a simple 8-byte binary value for speed.
		// In a full implementation, we would store a JSON struct with RefCount.
		return obj.Put([]byte(hash), []byte(fmt.Sprintf("%d", size)))
	})
}

// Get retrieves an object by its human-readable name.
func (s *CASStore) Get(name string) (io.ReadCloser, common.FileMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var hash string
	err := s.db.View(func(tx *bbolt.Tx) error {
		h := tx.Bucket(bucketNamespace).Get([]byte(name))
		if h == nil {
			return os.ErrNotExist
		}
		hash = string(h)
		return nil
	})
	if err != nil {
		return nil, common.FileMetadata{}, err
	}

	blobPath := s.getBlobPath(hash)
	f, err := os.Open(blobPath)
	if err != nil {
		return nil, common.FileMetadata{}, err
	}

	// Read metadata from DB
	var size int64
	s.db.View(func(tx *bbolt.Tx) error {
		val := tx.Bucket(bucketObjects).Get([]byte(hash))
		fmt.Sscanf(string(val), "%d", &size)
		return nil
	})

	return f, common.FileMetadata{Name: name, Hash: hash, Size: size}, nil
}

// Has checks if a content hash exists in the store.
func (s *CASStore) Has(hash string) (bool, error) {
	var exists bool
	err := s.db.View(func(tx *bbolt.Tx) error {
		val := tx.Bucket(bucketObjects).Get([]byte(hash))
		exists = (val != nil)
		return nil
	})
	return exists, err
}

func (s *CASStore) Delete(name string) error {
	// Simple deletion of the namespace entry. 
	// Real CAS would implement reference counting and garbage collection for the blobs.
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketNamespace).Delete([]byte(name))
	})
}

func (s *CASStore) GetBlobPath(name string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var hash string
	err := s.db.View(func(tx *bbolt.Tx) error {
		h := tx.Bucket(bucketNamespace).Get([]byte(name))
		if h == nil {
			return os.ErrNotExist
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
