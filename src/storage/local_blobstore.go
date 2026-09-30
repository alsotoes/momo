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
	// syncEnabled controls the per-blob fsync inside PutBlob (R3, #931).
	// Default true (historical fully-durable behavior). When the store is
	// configured with an R3 durability barrier the factory disables it so the
	// barrier owns durability at the ack boundary (fsync / group-commit /
	// none) instead of double-fsyncing.
	syncEnabled bool
}

// NewLocalBlobStore creates a new LocalBlobStore rooted at dataDir.
func NewLocalBlobStore(dataDir string) (*LocalBlobStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data dir: %w", syscall.EIO)
	}
	return &LocalBlobStore{base: dataDir, syncEnabled: true}, nil
}

// SetSyncEnabled toggles the per-blob fsync inside PutBlob (R3, #931). Used by
// the factory when an R3 durability barrier is installed.
func (b *LocalBlobStore) SetSyncEnabled(on bool) { b.syncEnabled = on }

// SyncBlob fsyncs the file backing hash (R3 fsync barrier, #931).
func (b *LocalBlobStore) SyncBlob(hash string) error {
	f, err := os.Open(b.blobPath(hash))
	if err != nil {
		return fmt.Errorf("durability: open blob to sync %s: %w", hash, err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("durability: fsync blob %s: %w", hash, err)
	}
	return nil
}

// SyncDir fsyncs the parent directory of hash (R3 group-commit barrier): makes
// the blob's atomic rename durable without a per-blob data-file fsync.
func (b *LocalBlobStore) SyncDir(hash string) error {
	d, err := os.Open(filepath.Dir(b.blobPath(hash)))
	if err != nil {
		return fmt.Errorf("durability: open blob dir for %s: %w", hash, err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("durability: fsync blob dir for %s: %w", hash, err)
	}
	return nil
}

// Compile-time assertion that LocalBlobStore exposes the R3 durability ops.
var _ DurabilityOps = (*LocalBlobStore)(nil)

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
		// Preserve the real error (e.g. a read error from content, or the
		// write-side syscall like ENOSPC) instead of masking it as ENOSPC.
		return fmt.Errorf("storage error: failed to write blob: %v: %w", err, syscall.EIO)
	}

	if err := writer.Flush(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("storage error: failed to flush blob: %w", syscall.EIO)
	}

	// Per-blob fsync is the durable default (R3 fsync mode). The factory
	// disables it when an R3 durability barrier owns durability at the ack
	// boundary (group-commit / none), to avoid double-fsyncing.
	if b.syncEnabled {
		if err := tmpFile.Sync(); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("storage error: failed to fsync blob: %w", syscall.EIO)
		}
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
func (b *LocalBlobStore) GetBlob(hash string) (rc io.ReadCloser, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in LocalBlobStore.GetBlob: %v", r)
			err = fmt.Errorf("panic in GetBlob: %v: %w", r, syscall.EIO)
		}
	}()

	if common.HasPathTraversalChars(hash) {
		return nil, fmt.Errorf("storage error: invalid hash contains path traversal characters: %w", syscall.EINVAL)
	}
	f, err := os.Open(b.blobPath(hash))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("storage error: failed to open blob: %w", syscall.EIO)
	}
	return f, nil
}

// DeleteBlob removes a blob by hash. Missing blobs are silently ignored.
func (b *LocalBlobStore) DeleteBlob(hash string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in LocalBlobStore.DeleteBlob: %v", r)
			err = fmt.Errorf("panic in DeleteBlob: %v: %w", r, syscall.EIO)
		}
	}()

	if common.HasPathTraversalChars(hash) {
		return fmt.Errorf("storage error: invalid hash contains path traversal characters: %w", syscall.EINVAL)
	}
	err = os.Remove(b.blobPath(hash))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage error: failed to delete blob: %w", syscall.EIO)
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
