package storage

import (
	"bytes"
	"fmt"
	"io"
	"syscall"

	"github.com/alsotoes/momo/src/common"
)

// AdaptiveBlobStore is the Rule 74 seam over the volume superblock layout and
// a per-blob fallback store (R-VS, #1128). Blobs at or below the size
// threshold are packed into append-only volume files for O(1) single-seek
// reads and hole-punching deletion; larger blobs go to the fallback store so
// the volume layout never carries oversized payloads.
type AdaptiveBlobStore struct {
	volume    *VolumeBlobStore
	fallback  BlobStore
	threshold int64
}

// Compile-time proof both implementations satisfy the BlobStore seam (Rule 74).
var (
	_ BlobStore = (*VolumeBlobStore)(nil)
	_ BlobStore = (*AdaptiveBlobStore)(nil)
)

// NewAdaptiveBlobStore routes blobs to volume (at or below threshold bytes)
// and to fallback (larger). Either side may be nil to force all traffic to
// the other, which keeps the seam usable in tests and degraded setups.
func NewAdaptiveBlobStore(volume *VolumeBlobStore, fallback BlobStore, threshold int64) *AdaptiveBlobStore {
	if threshold <= 0 {
		threshold = common.DefaultAdaptiveThreshold
	}
	return &AdaptiveBlobStore{
		volume:    volume,
		fallback:  fallback,
		threshold: threshold,
	}
}

// PutBlob buffers up to threshold+1 bytes to learn the object size before
// choosing a backend, then replays the buffered bytes ahead of the remaining
// stream so neither backend sees a partial read.
func (a *AdaptiveBlobStore) PutBlob(hash string, content io.Reader) (err error) {
	defer common.RecoverErr("AdaptiveBlobStore.PutBlob", &err)

	if common.HasPathTraversalChars(hash) {
		return fmt.Errorf("invalid hash contains path traversal characters: %w", syscall.EINVAL)
	}
	if a.volume == nil && a.fallback == nil {
		return fmt.Errorf("adaptive blob store has no backends: %w", syscall.EINVAL)
	}

	head := make([]byte, a.threshold+1)
	n, rerr := io.ReadFull(content, head)
	if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
		return fmt.Errorf("failed to read blob head: %w", rerr)
	}
	buf := head[:n]
	reader := io.MultiReader(bytes.NewReader(buf), content)

	if int64(n) <= a.threshold {
		if a.volume == nil {
			return fmt.Errorf("volume backend unavailable for blob %s: %w", common.SanitizeLog(hash), syscall.ENOSYS)
		}
		return a.volume.PutBlob(hash, reader)
	}
	if a.fallback == nil {
		return fmt.Errorf("fallback backend unavailable for blob %s: %w", common.SanitizeLog(hash), syscall.ENOSYS)
	}
	return a.fallback.PutBlob(hash, reader)
}

// GetBlob resolves from the volume store first; a miss (ENOENT) falls back.
func (a *AdaptiveBlobStore) GetBlob(hash string) (rc io.ReadCloser, err error) {
	defer common.RecoverErr("AdaptiveBlobStore.GetBlob", &err)

	if common.HasPathTraversalChars(hash) {
		return nil, fmt.Errorf("invalid hash contains path traversal characters: %w", syscall.EINVAL)
	}
	if a.volume != nil {
		if rc, err := a.volume.GetBlob(hash); err == nil {
			return rc, nil
		} else if err != syscall.ENOENT {
			return nil, err
		}
	}
	if a.fallback == nil {
		return nil, syscall.ENOENT
	}
	return a.fallback.GetBlob(hash)
}

// DeleteBlob removes the blob from both backends. Missing blobs are a no-op,
// per the BlobStore contract.
func (a *AdaptiveBlobStore) DeleteBlob(hash string) (err error) {
	defer common.RecoverErr("AdaptiveBlobStore.DeleteBlob", &err)

	if common.HasPathTraversalChars(hash) {
		return fmt.Errorf("invalid hash contains path traversal characters: %w", syscall.EINVAL)
	}

	var lastErr error
	if a.volume != nil {
		if err := a.volume.DeleteBlob(hash); err != nil && err != syscall.ENOENT {
			lastErr = err
		}
	}
	if a.fallback != nil {
		if err := a.fallback.DeleteBlob(hash); err != nil && err != syscall.ENOENT {
			lastErr = err
		}
	}
	return lastErr
}

// Close releases both backends.
func (a *AdaptiveBlobStore) Close() error {
	if a.volume != nil {
		if err := a.volume.Close(); err != nil {
			return err
		}
	}
	if a.fallback != nil {
		return a.fallback.Close()
	}
	return nil
}
