package storage

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func testEncKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	return hex.EncodeToString(key)
}

func TestEncryptedBlobStore_PutGetRoundTrip(t *testing.T) {
	encKey := testEncKey(t)
	tmpDir := t.TempDir()

	inner, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalBlobStore: %v", err)
	}
	defer inner.Close()

	enc, err := NewEncryptedBlobStore(inner, encKey)
	if err != nil {
		t.Fatalf("NewEncryptedBlobStore: %v", err)
	}

	plaintext := []byte("hello encrypted world")
	hash := "abc123def456"

	if err := enc.PutBlob(hash, bytes.NewReader(plaintext)); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	rc, err := enc.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round-trip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestEncryptedBlobStore_LargeFileRoundTrip(t *testing.T) {
	encKey := testEncKey(t)
	tmpDir := t.TempDir()

	inner, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalBlobStore: %v", err)
	}
	defer inner.Close()

	enc, err := NewEncryptedBlobStore(inner, encKey)
	if err != nil {
		t.Fatalf("NewEncryptedBlobStore: %v", err)
	}

	plaintext := make([]byte, 100*1024)
	for i := range plaintext {
		plaintext[i] = byte(i % 256)
	}
	hash := "largefilehash"

	if err := enc.PutBlob(hash, bytes.NewReader(plaintext)); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	rc, err := enc.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !bytes.Equal(got, plaintext) {
		t.Fatalf("large file round-trip mismatch: len got=%d, want=%d", len(got), len(plaintext))
	}
}

func TestEncryptedBlobStore_CiphertextDiffersFromPlaintext(t *testing.T) {
	encKey := testEncKey(t)
	tmpDir := t.TempDir()

	inner, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalBlobStore: %v", err)
	}
	defer inner.Close()

	enc, err := NewEncryptedBlobStore(inner, encKey)
	if err != nil {
		t.Fatalf("NewEncryptedBlobStore: %v", err)
	}

	plaintext := []byte("server-side encryption at rest")
	hash := "ssehash"

	if err := enc.PutBlob(hash, bytes.NewReader(plaintext)); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	rawRC, err := inner.GetBlob(hash)
	if err != nil {
		t.Fatalf("inner GetBlob: %v", err)
	}
	defer rawRC.Close()

	rawData, err := io.ReadAll(rawRC)
	if err != nil {
		t.Fatalf("ReadAll raw: %v", err)
	}

	if bytes.Equal(rawData, plaintext) {
		t.Fatal("ciphertext matches plaintext — encryption not applied")
	}
}

func TestEncryptedBlobStore_DeletePassthrough(t *testing.T) {
	encKey := testEncKey(t)
	tmpDir := t.TempDir()

	inner, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalBlobStore: %v", err)
	}
	defer inner.Close()

	enc, err := NewEncryptedBlobStore(inner, encKey)
	if err != nil {
		t.Fatalf("NewEncryptedBlobStore: %v", err)
	}

	hash := "deletehash"
	if err := enc.PutBlob(hash, bytes.NewReader([]byte("data"))); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	if err := enc.DeleteBlob(hash); err != nil {
		t.Fatalf("DeleteBlob: %v", err)
	}

	_, err = enc.GetBlob(hash)
	if err == nil {
		t.Fatal("GetBlob after delete should fail")
	}
}

func TestEncryptedBlobStore_DedupWorksOnPlaintextHash(t *testing.T) {
	encKey := testEncKey(t)
	tmpDir := t.TempDir()

	inner, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalBlobStore: %v", err)
	}
	defer inner.Close()

	enc, err := NewEncryptedBlobStore(inner, encKey)
	if err != nil {
		t.Fatalf("NewEncryptedBlobStore: %v", err)
	}

	plaintext := []byte("dedup content")
	hash := "plaintextHash"

	if err := enc.PutBlob(hash, bytes.NewReader(plaintext)); err != nil {
		t.Fatalf("PutBlob first: %v", err)
	}

	if err := enc.PutBlob(hash, bytes.NewReader(plaintext)); err != nil {
		t.Fatalf("PutBlob second: %v", err)
	}

	rc, err := enc.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !bytes.Equal(got, plaintext) {
		t.Fatalf("dedup round-trip mismatch")
	}
}

func TestEncryptedBlobStore_EmptyContent(t *testing.T) {
	encKey := testEncKey(t)
	tmpDir := t.TempDir()

	inner, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalBlobStore: %v", err)
	}
	defer inner.Close()

	enc, err := NewEncryptedBlobStore(inner, encKey)
	if err != nil {
		t.Fatalf("NewEncryptedBlobStore: %v", err)
	}

	hash := "emptyhash"
	if err := enc.PutBlob(hash, bytes.NewReader(nil)); err != nil {
		t.Fatalf("PutBlob empty: %v", err)
	}

	rc, err := enc.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("empty content round-trip: got %d bytes, want 0", len(got))
	}
}

func TestEncryptedBlobStore_TamperDetection(t *testing.T) {
	encKey := testEncKey(t)
	tmpDir := t.TempDir()

	inner, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalBlobStore: %v", err)
	}
	defer inner.Close()

	enc, err := NewEncryptedBlobStore(inner, encKey)
	if err != nil {
		t.Fatalf("NewEncryptedBlobStore: %v", err)
	}

	plaintext := []byte("tamper test content")
	hash := "tamperhash"

	if err := enc.PutBlob(hash, bytes.NewReader(plaintext)); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	blobPath := filepath.Join(tmpDir, "blobs", "ta", "mp", "er", hash)
	data, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if len(data) > 10 {
		data[10] ^= 0xFF
	}
	if err := os.WriteFile(blobPath, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rc, err := enc.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	defer rc.Close()

	_, err = io.ReadAll(rc)
	if err == nil {
		t.Fatal("tampered ciphertext should fail decryption")
	}
}

func TestEncryptedBlobStore_InvalidKey(t *testing.T) {
	tmpDir := t.TempDir()

	inner, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalBlobStore: %v", err)
	}
	defer inner.Close()

	_, err = NewEncryptedBlobStore(inner, "invalid-hex-key")
	if err == nil {
		t.Fatal("invalid key should fail")
	}
}

// failingInnerStore is a BlobStore whose PutBlob always fails, simulating a
// storage backend writing error (e.g. disk full) mid-write.
type failingInnerStore struct {
	putCalls atomic.Int32
}

func (f *failingInnerStore) PutBlob(hash string, content io.Reader) error {
	f.putCalls.Add(1)
	return os.ErrPermission
}

func (f *failingInnerStore) GetBlob(hash string) (io.ReadCloser, error) {
	return nil, os.ErrNotExist
}

func (f *failingInnerStore) DeleteBlob(hash string) error { return nil }
func (f *failingInnerStore) Close() error                 { return nil }

// blockingReader never returns any data on Read. It simulates a content source
// that is stuck (e.g. waiting for network data that never arrives).
type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) {
	select {}
}

// TestEncryptedBlobStore_PutBlobNoGoroutineLeakOnError verifies that when
// inner.PutBlob fails while the encryption goroutine is blocked reading the
// content source, the goroutine is cancelled and does not leak.
func TestEncryptedBlobStore_PutBlobNoGoroutineLeakOnError(t *testing.T) {
	defer goleak.VerifyNone(t)

	enc, err := NewEncryptedBlobStore(&failingInnerStore{}, testEncKey(t))
	if err != nil {
		t.Fatalf("NewEncryptedBlobStore: %v", err)
	}

	// The content source blocks forever; without cancellation the encryption
	// goroutine would hang. PutBlob must fail and release the goroutine.
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := enc.PutBlob("somehash", blockingReader{})
		if err == nil {
			t.Error("PutBlob should fail when the inner store fails")
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("PutBlob did not return — encryption goroutine likely leaked")
	}
}
