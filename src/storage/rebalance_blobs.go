package storage

import (
	"fmt"
	"io"
	"syscall"

	"github.com/alsotoes/momo/src/common"
)

// BlobInfo describes one locally stored blob, for the rebalance loop (R11).
type BlobInfo struct {
	Hash    string
	Size    int64
	ModTime int64 // unix nano, used as the coldness proxy
}

// blobWalker is the Rule 74 seam for backends that can enumerate their own
// bytes. Backends that cannot (S3, raw) simply do not support rebalancing.
type blobWalker interface {
	WalkBlobs(fn func(hash string, size, modTime int64) error) error
}

// ListBlobs returns every blob physically stored by this node. It fails with
// ENOTSUP when the backend cannot enumerate blobs.
func (s *CASStore) ListBlobs() (blobs []BlobInfo, err error) {
	defer common.RecoverErr("CASStore.ListBlobs", &err)
	walker, ok := s.blobs.(blobWalker)
	if !ok {
		return nil, fmt.Errorf("blob backend cannot enumerate blobs: %w", syscall.ENOTSUP)
	}
	err = walker.WalkBlobs(func(hash string, size, modTime int64) error {
		blobs = append(blobs, BlobInfo{Hash: hash, Size: size, ModTime: modTime})
		return nil
	})
	return blobs, err
}

// GetBlobByHash opens the local copy of a blob by content hash. It bypasses
// namespace metadata: the rebalance loop works on physical bytes.
func (s *CASStore) GetBlobByHash(hash string) (rc io.ReadCloser, err error) {
	defer common.RecoverErr("CASStore.GetBlobByHash", &err)
	return s.blobs.GetBlob(hash)
}

// DropBlob removes the local copy of a blob's bytes without touching metadata.
// The rebalance loop calls it only after a peer acknowledged holding the blob,
// so the object stays readable through the survivor path (R2-C1).
func (s *CASStore) DropBlob(hash string) (err error) {
	defer common.RecoverErr("CASStore.DropBlob", &err)
	return s.blobs.DeleteBlob(hash)
}
