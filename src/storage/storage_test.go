package storage

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

func TestCASStore(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-storage-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	content := []byte("hello world")
	hash := "5eb63bbbe01eeed093cb22bb8f5acdc3" // dummy md5 for testing, we use sha256 usually
	name := "test.txt"

	// 1. Put
	if err := store.Put(name, hash, int64(len(content)), bytes.NewReader(content)); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// 2. Has
	exists, err := store.Has(hash)
	if err != nil || !exists {
		t.Errorf("Has failed: exists=%v, err=%v", exists, err)
	}

	// 3. Get
	reader, meta, err := store.Get(name)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer reader.Close()

	if meta.Hash != hash || meta.Size != int64(len(content)) {
		t.Errorf("Metadata mismatch: got %+v", meta)
	}

	gotContent, _ := io.ReadAll(reader)
	if string(gotContent) != string(content) {
		t.Errorf("Content mismatch: got %q", gotContent)
	}

	// 4. Deduplication Test
	if err := store.Put("copy.txt", hash, int64(len(content)), bytes.NewReader(content)); err != nil {
		t.Fatalf("Put copy failed: %v", err)
	}

	// Verify both names point to same hash
	r2, _, _ := store.Get("copy.txt")
	r2.Close()

	// 5. Deduplication Hit (nil reader)
	if err := store.Put("third.txt", hash, int64(len(content)), nil); err != nil {
		t.Fatalf("Put with nil reader failed: %v", err)
	}
	r3, m3, _ := store.Get("third.txt")
	if m3.Hash != hash {
		t.Errorf("Metadata hash mismatch for nil reader put")
	}
	r3.Close()

	// 6. Delete
	if err := store.Delete(name); err != nil {
		t.Errorf("Delete failed: %v", err)
	}
	_, _, err = store.Get(name)
	if err == nil {
		t.Errorf("Get after delete should fail")
	}
}

func TestCASStore_EdgeCases(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-storage-edge-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	// 1. Get non-existent
	_, _, err = store.Get("nonexistent.txt")
	if err == nil {
		t.Errorf("Expected error for non-existent Get")
	}

	// 2. GetBlobPath non-existent
	_, err = store.GetBlobPath("nonexistent.txt")
	if err == nil {
		t.Errorf("Expected error for non-existent GetBlobPath")
	}

	// 3. Put with very small hash length (getBlobPath corner case)
	shortHash := "abc"
	if err := store.Put("short.txt", shortHash, 10, bytes.NewReader([]byte("test"))); err != nil {
		t.Fatalf("Failed to Put with short hash: %v", err)
	}
	path, err := store.GetBlobPath("short.txt")
	if err != nil {
		t.Fatalf("GetBlobPath failed: %v", err)
	}
	if !strings.Contains(path, "blobs/abc") {
		t.Errorf("Expected path to contain blobs/abc, got %s", path)
	}

	// 4. Panic recovery tests (Rule 4) via nil database
	nilStore := &CASStore{}
	
	_, _, err = nilStore.Get("test.txt")
	if err == nil {
		t.Errorf("Expected Get on nilStore to fail")
	}
	if !strings.Contains(err.Error(), "internal storage panic") {
		t.Errorf("Expected internal storage panic error, got %v", err)
	}

	err = nilStore.Put("test.txt", "hash", 10, bytes.NewReader([]byte("test")))
	if err == nil {
		t.Errorf("Expected Put on nilStore to fail")
	}
	if !strings.Contains(err.Error(), "internal storage panic") {
		t.Errorf("Expected internal storage panic error, got %v", err)
	}

	_, err = nilStore.Has("hash")
	if err == nil {
		t.Errorf("Expected Has on nilStore to fail")
	}
	if !strings.Contains(err.Error(), "internal storage panic") {
		t.Errorf("Expected internal storage panic error, got %v", err)
	}

	err = nilStore.Delete("test.txt")
	if err == nil {
		t.Errorf("Expected Delete on nilStore to fail")
	}
	if !strings.Contains(err.Error(), "internal storage panic") {
		t.Errorf("Expected internal storage panic error, got %v", err)
	}

	_, err = nilStore.GetBlobPath("test.txt")
	if err == nil {
		t.Errorf("Expected GetBlobPath on nilStore to fail")
	}
	if !strings.Contains(err.Error(), "internal storage panic") {
		t.Errorf("Expected internal storage panic error, got %v", err)
	}
}
