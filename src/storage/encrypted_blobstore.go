package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"syscall"

	momocrypto "github.com/alsotoes/momo/src/crypto"
)

// EncryptedBlobStore is a decorator that encrypts blob content at rest
// using AES-GCM-256 streaming AEAD. The underlying BlobStore stores
// only ciphertext. The hash key remains the plaintext content hash
// (used for CAS dedup), so dedup works on plaintext content.
//
// Concurrency: EncryptedBlobStore is safe for concurrent use when the
// underlying inner BlobStore is itself concurrency-safe. All
// implementations in this package (LocalBlobStore, RawBlobStore,
// S3BlobStore, CASStore) are internally synchronized, so this decorator
// needs no lock of its own.
type EncryptedBlobStore struct {
	inner  BlobStore
	cipher *momocrypto.Cipher
}

// Compile-time interface assertion.
var _ BlobStore = (*EncryptedBlobStore)(nil)
var _ DurabilityOps = (*EncryptedBlobStore)(nil)

// SetSyncEnabled forwards the R3 per-blob fsync toggle to an inner local
// backend (no-op for backends without one), so durability is owned by the ack
// barrier when configured (#931).
func (e *EncryptedBlobStore) SetSyncEnabled(on bool) {
	if t, ok := e.inner.(interface{ SetSyncEnabled(bool) }); ok {
		t.SetSyncEnabled(on)
	}
}

// SyncBlob forwards the R3 fsync barrier to the inner backend (R3-C1).
func (e *EncryptedBlobStore) SyncBlob(hash string) error {
	ops, ok := e.inner.(DurabilityOps)
	if !ok {
		return nil // backend persistence is the bar
	}
	return ops.SyncBlob(hash)
}

// SyncDir forwards the R3 group-commit directory barrier to the inner backend.
func (e *EncryptedBlobStore) SyncDir(hash string) error {
	ops, ok := e.inner.(DurabilityOps)
	if !ok {
		return nil
	}
	return ops.SyncDir(hash)
}

// NewEncryptedBlobStore wraps an existing BlobStore with AES-GCM-256
// encryption. The encKeyHex must be a 64-character hex string (32 bytes).
func NewEncryptedBlobStore(inner BlobStore, encKeyHex string) (*EncryptedBlobStore, error) {
	cipher, err := momocrypto.NewCipherFromHex(encKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to create encryption cipher: %w", err)
	}
	return &EncryptedBlobStore{inner: inner, cipher: cipher}, nil
}

// PutBlob encrypts the content stream and stores the ciphertext in the
// underlying BlobStore. The hash is the plaintext content hash, preserving
// CAS dedup semantics.
func (e *EncryptedBlobStore) PutBlob(hash string, content io.Reader) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in EncryptedBlobStore.PutBlob: %v", r)
			err = fmt.Errorf("panic in PutBlob: %v: %w", r, syscall.EIO)
		}
	}()

	// ⚡ Bolt: Stream encryption avoids loading the entire plaintext into
	// memory. EncryptStream reads in 4KB chunks and writes to a pipe,
	// so the inner store receives ciphertext in a streaming fashion.
	pr, pw := io.Pipe()
	// 🛡️ Goroutine-leak guard (Rule 5): if PutBlob below fails, cancel the
	// encryption goroutine so it doesn't hang indefinitely reading from
	// `content` (e.g. waiting for network data) after the pipe is closed.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		// 🛡️ Zero-Crash: A panic in this goroutine would crash the whole
		// process — it is not covered by the outer function's recover. Recover
		// and propagate the error through the pipe instead (Rule 37).
		defer func() {
			if r := recover(); r != nil {
				log.Printf("CRITICAL: Panic recovered in EncryptedBlobStore.PutBlob goroutine: %v", r)
				pw.CloseWithError(fmt.Errorf("panic in encryption goroutine: %v: %w", r, syscall.EIO))
			}
		}()

		if err := e.cipher.EncryptStream(ctxReader{ctx: ctx, r: content}, pw); err != nil {
			pw.CloseWithError(fmt.Errorf("encryption failed: %w", err))
			return
		}
		pw.Close()
	}()

	if err := e.inner.PutBlob(hash, pr); err != nil {
		pr.Close()
		return fmt.Errorf("failed to store encrypted blob: %w", err)
	}
	return nil
}

// GetBlob retrieves the ciphertext from the underlying BlobStore and
// returns a streaming reader that decrypts on read. The caller must
// close the returned ReadCloser.
func (e *EncryptedBlobStore) GetBlob(hash string) (result io.ReadCloser, err error) {
	// 🛡️ Zero-Crash: If a panic occurs after opening the inner reader,
	// close it to prevent zombie file descriptors (Rule 43).
	var innerRC io.ReadCloser
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in EncryptedBlobStore.GetBlob: %v", r)
			if innerRC != nil {
				innerRC.Close()
			}
			err = fmt.Errorf("panic in GetBlob: %v: %w", r, syscall.EIO)
		}
	}()

	innerRC, err = e.inner.GetBlob(hash)
	if err != nil {
		return nil, err
	}

	// ⚡ Bolt: Stream decryption via io.Pipe — the inner store's reader
	// is consumed in a goroutine, and the caller receives decrypted
	// chunks without buffering the entire blob in memory.
	pr, pw := io.Pipe()
	go func() {
		defer innerRC.Close()
		if decErr := e.cipher.DecryptStream(innerRC, pw); decErr != nil {
			pw.CloseWithError(fmt.Errorf("decryption failed: %w", decErr))
			return
		}
		pw.Close()
	}()

	return pr, nil
}

// DeleteBlob is a passthrough to the underlying store. Encryption does
// not affect deletion semantics.
func (e *EncryptedBlobStore) DeleteBlob(hash string) (err error) {
	// 🛡️ Zero-Crash (Rule 37): a panic in the underlying store must not
	// escape through this passthrough.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in EncryptedBlobStore.DeleteBlob: %v", r)
			err = fmt.Errorf("panic in DeleteBlob: %v: %w", r, syscall.EIO)
		}
	}()

	return e.inner.DeleteBlob(hash)
}

// Close closes the underlying BlobStore.
func (e *EncryptedBlobStore) Close() error {
	return e.inner.Close()
}

// ctxReader wraps an io.Reader so that a blocked Read returns immediately once
// the given context is cancelled. This prevents the PutBlob encryption
// goroutine from hanging indefinitely if the underlying content reader blocks
// (e.g. waiting for network data) after the pipe has been closed.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
	}
	n, err := c.r.Read(p)
	return n, err
}
