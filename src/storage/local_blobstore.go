package storage

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"syscall"

	"github.com/alsotoes/momo/src/common"
)

// LocalBlobStore implements BlobStore using the local filesystem with
// tiered directory fan-out for content-addressed blob storage.
// Blob path layout: <base>/blobs/ab/cd/ef/<full-hash>
type LocalBlobStore struct {
	base string
}

// NewLocalBlobStore creates a new LocalBlobStore rooted at dataDir.
func NewLocalBlobStore(dataDir string) (*LocalBlobStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data dir: %w", syscall.EIO)
	}
	return &LocalBlobStore{base: dataDir}, nil
}

// PutBlob writes a blob atomically using temp file + rename.
func (b *LocalBlobStore) PutBlob(hash string, content io.Reader) (err error) {
	var tmpFile *os.File
	var tmpPath string

	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in LocalBlobStore.PutBlob: %v", r)
			if tmpFile != nil {
				tmpFile.Close()
			}
			if tmpPath != "" {
				os.Remove(tmpPath)
			}
			err = fmt.Errorf("panic in PutBlob: %v: %w", r, syscall.EIO)
		}
	}()

	if common.HasPathTraversalChars(hash) {
		return fmt.Errorf("storage error: invalid hash contains path traversal characters: %w", syscall.EINVAL)
	}

	blobPath := b.blobPath(hash)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0755); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("storage error: failed to create tiered dir: %w", syscall.EACCES)
		}
		return fmt.Errorf("storage error: failed to create tiered dir: %w", syscall.EIO)
	}

	tmpFile, err = os.CreateTemp(b.base, "blob-*.tmp")
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("storage error: failed to create temp file: %w", syscall.EACCES)
		}
		return fmt.Errorf("storage error: failed to create temp file: %w", syscall.EIO)
	}
	tmpPath = tmpFile.Name()

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

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("storage error: failed to fsync blob: %w", syscall.EIO)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("storage error: failed to close blob: %w", syscall.EIO)
	}

	if err := os.Rename(tmpPath, blobPath); err != nil {
		os.Remove(tmpPath)
		if os.IsPermission(err) {
			return fmt.Errorf("storage error: failed to commit blob: %w", syscall.EACCES)
		}
		return fmt.Errorf("storage error: failed to commit blob: %w", syscall.EIO)
	}
	return nil
}

// GetBlob opens a blob for reading by its content hash.
func (b *LocalBlobStore) GetBlob(hash string) (io.ReadCloser, error) {
	if common.HasPathTraversalChars(hash) {
		return nil, fmt.Errorf("storage error: invalid hash contains path traversal characters: %w", syscall.EINVAL)
	}
	return os.Open(b.blobPath(hash))
}

// DeleteBlob removes a blob by hash. Missing blobs are silently ignored.
func (b *LocalBlobStore) DeleteBlob(hash string) error {
	if common.HasPathTraversalChars(hash) {
		return fmt.Errorf("storage error: invalid hash contains path traversal characters: %w", syscall.EINVAL)
	}
	err := os.Remove(b.blobPath(hash))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Close is a no-op for LocalBlobStore.
func (b *LocalBlobStore) Close() error {
	return nil
}

// blobPath transforms a hash into a tiered directory path.
// Hash "abcdef123..." -> "base/blobs/ab/cd/ef/abcdef123..."
func (b *LocalBlobStore) blobPath(hash string) string {
	if len(hash) < 6 {
		return filepath.Join(b.base, "blobs", hash)
	}
	return filepath.Join(b.base, "blobs", hash[0:2], hash[2:4], hash[4:6], hash)
}
