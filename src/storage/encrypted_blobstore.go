package storage

import (
	"fmt"
	"io"
	"log"
	"sync"
	"syscall"

	momocrypto "github.com/alsotoes/momo/src/crypto"
)

// EncryptedBlobStore is a decorator that encrypts blob content at rest
// using AES-GCM-256 streaming AEAD. The underlying BlobStore stores
// only ciphertext. The hash key remains the plaintext content hash
// (used for CAS dedup), so dedup works on plaintext content.
type EncryptedBlobStore struct {
	inner  BlobStore
	cipher *momocrypto.Cipher
	mu     sync.Mutex
}

// Compile-time interface assertion.
var _ BlobStore = (*EncryptedBlobStore)(nil)

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
func (e *EncryptedBlobStore) PutBlob(hash string, content io.Reader) error {
	if r := recover(); r != nil {
		log.Printf("CRITICAL: Panic recovered in EncryptedBlobStore.PutBlob: %v", r)
		return fmt.Errorf("panic in PutBlob: %v: %w", r, syscall.EIO)
	}

	// ⚡ Bolt: Stream encryption avoids loading the entire plaintext into
	// memory. EncryptStream reads in 4KB chunks and writes to a pipe,
	// so the inner store receives ciphertext in a streaming fashion.
	pr, pw := io.Pipe()
	go func() {
		if err := e.cipher.EncryptStream(content, pw); err != nil {
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
func (e *EncryptedBlobStore) GetBlob(hash string) (io.ReadCloser, error) {
	if r := recover(); r != nil {
		log.Printf("CRITICAL: Panic recovered in EncryptedBlobStore.GetBlob: %v", r)
		return nil, fmt.Errorf("panic in GetBlob: %v: %w", r, syscall.EIO)
	}

	rc, err := e.inner.GetBlob(hash)
	if err != nil {
		return nil, err
	}

	// ⚡ Bolt: Stream decryption via io.Pipe — the inner store's reader
	// is consumed in a goroutine, and the caller receives decrypted
	// chunks without buffering the entire blob in memory.
	pr, pw := io.Pipe()
	go func() {
		defer rc.Close()
		if err := e.cipher.DecryptStream(rc, pw); err != nil {
			pw.CloseWithError(fmt.Errorf("decryption failed: %w", err))
			return
		}
		pw.Close()
	}()

	return pr, nil
}

// DeleteBlob is a passthrough to the underlying store. Encryption does
// not affect deletion semantics.
func (e *EncryptedBlobStore) DeleteBlob(hash string) error {
	return e.inner.DeleteBlob(hash)
}

// Close closes the underlying BlobStore.
func (e *EncryptedBlobStore) Close() error {
	return e.inner.Close()
}
