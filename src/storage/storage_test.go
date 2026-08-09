package storage

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
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
	if err := store.Put(name, hash, int64(len(content)), "", bytes.NewReader(content)); err != nil {
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
	if err := store.Put("copy.txt", hash, int64(len(content)), "", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put copy failed: %v", err)
	}

	// Verify both names point to same hash
	r2, _, _ := store.Get("copy.txt")
	r2.Close()

	// 5. Deduplication Hit (nil reader)
	if err := store.Put("third.txt", hash, int64(len(content)), "", nil); err != nil {
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

func TestCASStore_RemotePath(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-storage-path-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	content := []byte("hello world path")
	hash := "5eb63bbbe01eeed093cb22bb8f5acdc3" // notsecret
	name := "path-test.txt"

	// 1. Put with RemotePath containing slashes/spaces (normalization check)
	rawPath := " /customer01//documents/invoice.pdf/ "
	expectedNormalizedPath := "customer01/documents/invoice.pdf"

	if err := store.Put(name, hash, int64(len(content)), rawPath, bytes.NewReader(content)); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// 2. Get and Verify RemotePath
	reader, meta, err := store.Get(name)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer reader.Close()

	if meta.RemotePath != expectedNormalizedPath {
		t.Errorf("Expected RemotePath %q, got %q", expectedNormalizedPath, meta.RemotePath)
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
	if err := store.Put("short.txt", shortHash, 10, "", bytes.NewReader([]byte("test"))); err != nil {
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

	err = nilStore.Put("test.txt", "hash", 10, "", bytes.NewReader([]byte("test")))
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

	_, err = nilStore.List()
	if err == nil {
		t.Errorf("Expected List on nilStore to fail")
	}
	if !strings.Contains(err.Error(), "internal storage panic") {
		t.Errorf("Expected internal storage panic error, got %v", err)
	}
}

func TestCASStore_List(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-storage-list-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	// Put some files
	filesToPut := []struct {
		name string
		hash string
		size int64
		path string
	}{
		{"file1.txt", "hash1", 100, "docs"},
		{"file2.txt", "hash2", 200, "images"},
		{"file3.txt", "hash3", 300, ""},
	}

	for _, f := range filesToPut {
		err := store.Put(f.name, f.hash, f.size, f.path, bytes.NewReader(make([]byte, f.size)))
		if err != nil {
			t.Fatalf("Failed to put file %s: %v", f.name, err)
		}
	}

	// List
	list, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(list) != 3 {
		t.Errorf("Expected 3 files, got %d", len(list))
	}

	found := make(map[string]bool)
	for _, f := range list {
		found[f.Name] = true
		if f.ModTime <= 0 {
			t.Errorf("Expected positive ModTime for %s, got %d", f.Name, f.ModTime)
		}
		// Verify properties
		switch f.Name {
		case "file1.txt":
			if f.Hash != "hash1" || f.Size != 100 || f.RemotePath != "docs" {
				t.Errorf("Unexpected metadata for file1.txt: %+v", f)
			}
		case "file2.txt":
			if f.Hash != "hash2" || f.Size != 200 || f.RemotePath != "images" {
				t.Errorf("Unexpected metadata for file2.txt: %+v", f)
			}
		case "file3.txt":
			if f.Hash != "hash3" || f.Size != 300 || f.RemotePath != "" {
				t.Errorf("Unexpected metadata for file3.txt: %+v", f)
			}
		default:
			t.Errorf("Found unexpected file in list: %s", f.Name)
		}
	}

	if len(found) != 3 {
		t.Errorf("Not all files were found in list: %+v", found)
	}
}

func TestCASStore_ModTime(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-storage-modtime-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	content := []byte("modtime content")
	hash := "dummyhash123"
	name := "modtime.txt"

	if err := store.Put(name, hash, int64(len(content)), "", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get should report the recorded ModTime.
	before := time.Now().Add(-5 * time.Second).UnixNano()
	_, meta, err := store.Get(name)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if meta.ModTime <= 0 {
		t.Errorf("Expected recorded ModTime, got %d", meta.ModTime)
	}
	if meta.ModTime < before || meta.ModTime > time.Now().Add(5*time.Second).UnixNano() {
		t.Errorf("ModTime %d outside expected window", meta.ModTime)
	}

	// Delete should remove the ModTime record.
	if err := store.Delete(name); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	for _, f := range list {
		if f.Name == name {
			t.Errorf("Expected %s to be removed after delete", name)
		}
	}
}

func TestCASStore_GetHashForName(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-hashname-test-*")
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
	hash := common.HashBytes(content)

	if err := store.Put("fileA.txt", hash, int64(len(content)), "", bytes.NewReader(content)); err != nil {
		t.Fatalf("Failed to Put fileA: %v", err)
	}

	gotHash, err := store.GetHashForName("fileA.txt")
	if err != nil {
		t.Fatalf("GetHashForName failed for existing name: %v", err)
	}
	if gotHash != hash {
		t.Errorf("Expected hash %s, got %s", hash, gotHash)
	}

	_, err = store.GetHashForName("fileB.txt")
	if err == nil {
		t.Errorf("Expected error for non-existent name fileB.txt")
	}
}

func TestCASStoreDeleteRemovesBlobImmediately(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-cve006-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	content := []byte("orphan me")
	hash := common.HashBytes(content)

	if err := store.Put("orphan.txt", hash, int64(len(content)), "", bytes.NewReader(content)); err != nil {
		t.Fatalf("Failed to Put: %v", err)
	}

	blobPath := store.getBlobPath(hash)
	if _, err := os.Stat(blobPath); os.IsNotExist(err) {
		t.Fatal("Blob file should exist before Delete")
	}

	if err := store.Delete("orphan.txt"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, err := os.Stat(blobPath); !os.IsNotExist(err) {
		t.Fatal("Blob file should be removed immediately by Delete (CVE-006)")
	}

	exists, _ := store.Has(hash)
	if exists {
		t.Fatal("Has should return false after Delete removed the blob")
	}
}

func TestCASStore_NamespaceCollision(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-ns-collision-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	contentA := []byte("content from dirA")
	contentB := []byte("content from dirB")
	hashA := common.HashBytes(contentA)
	hashB := common.HashBytes(contentB)

	keyA := "dirA/file.txt"
	keyB := "dirB/file.txt"

	if err := store.Put(keyA, hashA, int64(len(contentA)), "dirA", bytes.NewReader(contentA)); err != nil {
		t.Fatalf("Failed to Put %s: %v", keyA, err)
	}
	if err := store.Put(keyB, hashB, int64(len(contentB)), "dirB", bytes.NewReader(contentB)); err != nil {
		t.Fatalf("Failed to Put %s: %v", keyB, err)
	}

	readerA, metaA, err := store.Get(keyA)
	if err != nil {
		t.Fatalf("Failed to Get %s: %v", keyA, err)
	}
	defer readerA.Close()
	if metaA.Hash != hashA {
		t.Errorf("Expected hash %s for %s, got %s", hashA, keyA, metaA.Hash)
	}
	gotA, _ := io.ReadAll(readerA)
	if string(gotA) != string(contentA) {
		t.Errorf("Expected content %q for %s, got %q", contentA, keyA, gotA)
	}

	readerB, metaB, err := store.Get(keyB)
	if err != nil {
		t.Fatalf("Failed to Get %s: %v", keyB, err)
	}
	defer readerB.Close()
	if metaB.Hash != hashB {
		t.Errorf("Expected hash %s for %s, got %s", hashB, keyB, metaB.Hash)
	}
	gotB, _ := io.ReadAll(readerB)
	if string(gotB) != string(contentB) {
		t.Errorf("Expected content %q for %s, got %q", contentB, keyB, gotB)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("Expected 2 entries in list, got %d", len(list))
	}

	names := make(map[string]bool)
	for _, f := range list {
		names[f.Name] = true
	}
	if !names[keyA] {
		t.Errorf("Expected %s in list", keyA)
	}
	if !names[keyB] {
		t.Errorf("Expected %s in list", keyB)
	}

	hashA2, err := store.GetHashForName(keyA)
	if err != nil {
		t.Fatalf("GetHashForName failed for %s: %v", keyA, err)
	}
	if hashA2 != hashA {
		t.Errorf("Expected hash %s for %s, got %s", hashA, keyA, hashA2)
	}
	hashB2, err := store.GetHashForName(keyB)
	if err != nil {
		t.Fatalf("GetHashForName failed for %s: %v", keyB, err)
	}
	if hashB2 != hashB {
		t.Errorf("Expected hash %s for %s, got %s", hashB, keyB, hashB2)
	}
}
