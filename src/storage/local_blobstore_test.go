package storage

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	"go.uber.org/goleak"
)

func TestLocalBlobStore_PutGet(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-local-blob-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create LocalBlobStore: %v", err)
	}
	defer store.Close()

	content := []byte("hello world, this is a test blob")
	hash := "a94a8fe5ccb19ba61c4c0873d391e987982fbbd3"

	if err := store.PutBlob(hash, bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	reader, err := store.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob failed: %v", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("Content mismatch: got %q, want %q", got, content)
	}
}

func TestLocalBlobStore_PutLargeBlob(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-local-blob-large-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create LocalBlobStore: %v", err)
	}
	defer store.Close()

	content := bytes.Repeat([]byte("x"), 256*1024)
	hash := "largeblobhash1234567890abcdef"

	if err := store.PutBlob(hash, bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	reader, err := store.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob failed: %v", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if len(got) != len(content) {
		t.Errorf("Size mismatch: got %d, want %d", len(got), len(content))
	}
	if !bytes.Equal(got, content) {
		t.Errorf("Content mismatch")
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, e := range entries {
		if bytes.HasPrefix([]byte(e.Name()), []byte("blob-")) && bytes.HasSuffix([]byte(e.Name()), []byte(".tmp")) {
			t.Errorf("Temp file leaked: %s", e.Name())
		}
	}
}

func TestLocalBlobStore_Delete(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-local-blob-del-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create LocalBlobStore: %v", err)
	}
	defer store.Close()

	content := []byte("delete me")
	hash := "deletehash1234567890abcdef"

	if err := store.PutBlob(hash, bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	if err := store.DeleteBlob(hash); err != nil {
		t.Fatalf("DeleteBlob failed: %v", err)
	}

	if _, err := store.GetBlob(hash); err == nil {
		t.Errorf("GetBlob should fail after delete")
	}

	if err := store.DeleteBlob(hash); err != nil {
		t.Errorf("DeleteBlob should be no-op for missing blob, got: %v", err)
	}
}

func TestLocalBlobStore_GetNotFound(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-local-blob-nf-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create LocalBlobStore: %v", err)
	}
	defer store.Close()

	if _, err := store.GetBlob("nonexistenthash123456"); err == nil {
		t.Errorf("GetBlob should fail for non-existent blob")
	}
}

func TestLocalBlobStore_Dedup(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-local-blob-dedup-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create LocalBlobStore: %v", err)
	}
	defer store.Close()

	content := []byte("dedup content")
	hash := "deduphash1234567890abcdef"

	if err := store.PutBlob(hash, bytes.NewReader(content)); err != nil {
		t.Fatalf("First PutBlob failed: %v", err)
	}
	if err := store.PutBlob(hash, bytes.NewReader(content)); err != nil {
		t.Fatalf("Second PutBlob failed: %v", err)
	}

	reader, err := store.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob failed: %v", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("Content mismatch after dedup")
	}
}

func TestLocalBlobStore_PathTraversal(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-local-blob-pt-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create LocalBlobStore: %v", err)
	}
	defer store.Close()

	badHash := "../../../etc/passwd"

	// Test GetBlob path traversal
	if _, err := store.GetBlob(badHash); err == nil {
		t.Errorf("GetBlob with path traversal should fail")
	}

	// Test DeleteBlob path traversal
	if err := store.DeleteBlob(badHash); err == nil {
		t.Errorf("DeleteBlob with path traversal should fail")
	}

	// Double check that PutBlob is also correctly failing
	if err := store.PutBlob(badHash, bytes.NewReader([]byte("test"))); err == nil {
		t.Errorf("PutBlob with path traversal should fail")
	}
}

// failingReader returns an error after n bytes, simulating a source read
// failure (e.g. network error or context cancellation) mid-stream.
type failingReader struct {
	remaining int
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, syscall.ECONNRESET
	}
	if len(p) > r.remaining {
		p = p[:r.remaining]
	}
	r.remaining -= len(p)
	return len(p), nil
}

func TestLocalBlobStore_PutBlobSurfacesSourceReadError(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-local-blob-rd-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewLocalBlobStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create LocalBlobStore: %v", err)
	}
	defer store.Close()

	hash := "readerrorhash1234567890abcd"

	// The source read fails partway through; PutBlob must surface that error
	// instead of masking it as ENOSPC.
	err = store.PutBlob(hash, &failingReader{remaining: 10})
	if err == nil {
		t.Fatal("PutBlob should fail when the source reader errors")
	}
	if errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("PutBlob masked the source read error as ENOSPC: %v", err)
	}
	if !errors.Is(err, syscall.EIO) {
		t.Fatalf("PutBlob error should chain syscall.EIO, got: %v", err)
	}
	if !strings.Contains(err.Error(), syscall.ECONNRESET.Error()) {
		t.Fatalf("PutBlob error should mention the underlying cause (%v), got: %v", syscall.ECONNRESET, err)
	}

	// No temp files should be left behind on the failure path.
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, e := range entries {
		if bytes.HasPrefix([]byte(e.Name()), []byte("blob-")) && bytes.HasSuffix([]byte(e.Name()), []byte(".tmp")) {
			t.Errorf("Temp file leaked after PutBlob error: %s", e.Name())
		}
	}
}
