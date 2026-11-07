package storage

import "io"

// BlobStore defines the interface for content-addressed blob storage.
// Each backend (local filesystem, NFS, S3, raw device) implements this
// interface to store and retrieve blob data keyed by content hash.
// Metadata (refcounts, tombstones, namespace mappings) is handled by
// the CASStore wrapper via bbolt, so BlobStore implementations only
// manage raw blob bytes.
// blobPresence is the optional seam for backends that can distinguish "the
// metadata says the blob exists" from "the bytes are physically here". After a
// rebalance shed removes bytes but keeps metadata, an optimistic Put would
// acknowledge without storing anything — and the sender then drops its copy.
type blobPresence interface {
	BlobExists(hash string) (bool, error)
}

type BlobStore interface {
	io.Closer
	// PutBlob stores a blob identified by its content hash.
	// If a blob with the same hash already exists, implementations
	// may treat this as a no-op.
	PutBlob(hash string, content io.Reader) error
	// GetBlob retrieves a blob by its content hash.
	// The caller must close the returned ReadCloser.
	GetBlob(hash string) (io.ReadCloser, error)
	// DeleteBlob removes a blob by its content hash.
	// If the blob does not exist, implementations must treat this
	// as a no-op (return nil).
	DeleteBlob(hash string) error
}
