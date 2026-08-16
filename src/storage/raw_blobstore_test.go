package storage

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"go.etcd.io/bbolt"
	"go.uber.org/goleak"
)

func TestRawBlobStore_PutGetDelete(t *testing.T) {
	defer goleak.VerifyNone(t)
	tempDir := t.TempDir()

	devicePath := filepath.Join(tempDir, "fake-device")
	dataDir := filepath.Join(tempDir, "data")

	cfg := common.ConfigurationStorage{
		Backend:       "raw",
		RawDevicePath: devicePath,
	}
	daemon := &common.Daemon{Data: dataDir, Drive: devicePath}

	store, err := NewRawBlobStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewRawBlobStore failed: %v", err)
	}
	defer store.Close()

	content := []byte("hello raw device")
	hash := "rawhash123"

	// Put
	if err := store.PutBlob(hash, bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	// Get
	reader, err := store.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob failed: %v", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("Content mismatch: got %q, want %q", got, content)
	}

	// Delete
	if err := store.DeleteBlob(hash); err != nil {
		t.Fatalf("DeleteBlob failed: %v", err)
	}

	// Get after delete should return ENOENT
	_, err = store.GetBlob(hash)
	if err == nil {
		t.Errorf("Expected error after delete")
	}
}

func TestRawBlobStore_Dedup(t *testing.T) {
	defer goleak.VerifyNone(t)
	tempDir := t.TempDir()
	devicePath := filepath.Join(tempDir, "fake-device")
	dataDir := filepath.Join(tempDir, "data")

	cfg := common.ConfigurationStorage{Backend: "raw", RawDevicePath: devicePath}
	daemon := &common.Daemon{Data: dataDir}

	store, err := NewRawBlobStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewRawBlobStore failed: %v", err)
	}
	defer store.Close()

	content := []byte("dedup test")
	hash := "deduphash"

	if err := store.PutBlob(hash, bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	// Second put with same hash should be no-op
	if err := store.PutBlob(hash, bytes.NewReader([]byte("different"))); err != nil {
		t.Fatalf("PutBlob dedup failed: %v", err)
	}

	// Content should be original
	reader, _ := store.GetBlob(hash)
	defer reader.Close()
	got, _ := io.ReadAll(reader)
	if !bytes.Equal(got, content) {
		t.Errorf("Dedup failed: got %q, want %q", got, content)
	}
}

func TestRawBlobStore_DeleteMissing(t *testing.T) {
	defer goleak.VerifyNone(t)
	tempDir := t.TempDir()
	devicePath := filepath.Join(tempDir, "fake-device")
	dataDir := filepath.Join(tempDir, "data")

	cfg := common.ConfigurationStorage{Backend: "raw", RawDevicePath: devicePath}
	daemon := &common.Daemon{Data: dataDir}

	store, err := NewRawBlobStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewRawBlobStore failed: %v", err)
	}
	defer store.Close()

	if err := store.DeleteBlob("nonexistent"); err != nil {
		t.Errorf("DeleteBlob on missing should be nil, got: %v", err)
	}
}

func TestRawBlobStore_PersistenceAcrossRestart(t *testing.T) {
	tempDir := t.TempDir()
	devicePath := filepath.Join(tempDir, "fake-device")
	dataDir := filepath.Join(tempDir, "data")

	cfg := common.ConfigurationStorage{Backend: "raw", RawDevicePath: devicePath}
	daemon := &common.Daemon{Data: dataDir}

	content := []byte("persistent data")
	hash := "persisthash"

	// First instance
	store1, err := NewRawBlobStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewRawBlobStore failed: %v", err)
	}
	if err := store1.PutBlob(hash, bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}
	store1.Close()

	// Second instance (restart)
	store2, err := NewRawBlobStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewRawBlobStore on restart failed: %v", err)
	}
	defer store2.Close()

	reader, err := store2.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob after restart failed: %v", err)
	}
	defer reader.Close()

	got, _ := io.ReadAll(reader)
	if !bytes.Equal(got, content) {
		t.Errorf("Content mismatch after restart: got %q, want %q", got, content)
	}
}

func TestRawBlobStore_DriveFallback(t *testing.T) {
	defer goleak.VerifyNone(t)
	tempDir := t.TempDir()
	devicePath := filepath.Join(tempDir, "fake-device")
	dataDir := filepath.Join(tempDir, "data")

	// No RawDevicePath; should fall back to daemon.Drive
	cfg := common.ConfigurationStorage{Backend: "raw"}
	daemon := &common.Daemon{Data: dataDir, Drive: devicePath}

	store, err := NewRawBlobStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewRawBlobStore failed: %v", err)
	}
	defer store.Close()

	content := []byte("drive fallback")
	if err := store.PutBlob("drivehash", bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}
}

func TestRawBlobStore_MissingDevicePath(t *testing.T) {
	cfg := common.ConfigurationStorage{Backend: "raw"}
	daemon := &common.Daemon{Data: "/tmp"}

	_, err := NewRawBlobStore(cfg, daemon)
	if err == nil {
		t.Errorf("Expected error for missing device path")
	}
}

func TestRawBlobStore_PutLargeBlob(t *testing.T) {
	defer goleak.VerifyNone(t)
	tempDir := t.TempDir()
	devicePath := filepath.Join(tempDir, "fake-device")
	dataDir := filepath.Join(tempDir, "data")

	cfg := common.ConfigurationStorage{Backend: "raw", RawDevicePath: devicePath}
	daemon := &common.Daemon{Data: dataDir}

	store, err := NewRawBlobStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewRawBlobStore failed: %v", err)
	}
	defer store.Close()

	size := 10 * 1024 * 1024
	content := bytes.Repeat([]byte("A"), size)
	hash := "largeblobhash"

	if err := store.PutBlob(hash, bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob large failed: %v", err)
	}

	reader, err := store.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob large failed: %v", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read large blob: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("Large blob content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}
}

func TestRawBlobStore_CorruptedAllocTable(t *testing.T) {
	defer goleak.VerifyNone(t)
	tempDir := t.TempDir()
	devicePath := filepath.Join(tempDir, "fake-device")
	dataDir := filepath.Join(tempDir, "data")

	cfg := common.ConfigurationStorage{Backend: "raw", RawDevicePath: devicePath}
	daemon := &common.Daemon{Data: dataDir}

	store, err := NewRawBlobStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewRawBlobStore failed: %v", err)
	}
	defer store.Close()

	content := []byte("corrupt test")
	hash := "corrupthash"

	if err := store.PutBlob(hash, bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	// Corrupt the allocation table: set length to a huge value
	var alloc [16]byte
	binary.BigEndian.PutUint64(alloc[0:8], 0)
	binary.BigEndian.PutUint64(alloc[8:16], uint64(common.MaxFileSize+1))
	err = store.allocDB.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketRawAlloc).Put([]byte(hash), alloc[:])
	})
	if err != nil {
		t.Fatalf("Failed to corrupt alloc table: %v", err)
	}

	// GetBlob should return an error, not panic or OOM
	_, err = store.GetBlob(hash)
	if err == nil {
		t.Fatal("Expected error for corrupted alloc table, got nil")
	}

	// Corrupt with negative length (via uint64 that overflows int64)
	binary.BigEndian.PutUint64(alloc[8:16], 0xFFFFFFFFFFFFFFFF)
	err = store.allocDB.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketRawAlloc).Put([]byte(hash), alloc[:])
	})
	if err != nil {
		t.Fatalf("Failed to corrupt alloc table: %v", err)
	}

	_, err = store.GetBlob(hash)
	if err == nil {
		t.Fatal("Expected error for negative length, got nil")
	}
}

func TestNewStore_RawBackend(t *testing.T) {
	defer goleak.VerifyNone(t)
	tempDir := t.TempDir()
	devicePath := filepath.Join(tempDir, "fake-device")
	dataDir := filepath.Join(tempDir, "data")

	cfg := common.ConfigurationStorage{
		Backend:            "raw",
		RawDevicePath:      devicePath,
		GCInterval:         300,
		TombstoneRetention: 86400,
	}
	daemon := &common.Daemon{Data: dataDir}

	store, err := NewStore(cfg, daemon, "")
	if err != nil {
		t.Fatalf("NewStore with raw backend failed: %v", err)
	}
	defer store.Close()

	content := []byte("test via factory")
	if err := store.Put("file.txt", "rawhash123", int64(len(content)), "", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	reader, meta, err := store.Get("file.txt")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer reader.Close()

	if meta.Hash != "rawhash123" {
		t.Errorf("Expected hash rawhash123, got %s", meta.Hash)
	}

	got, _ := io.ReadAll(reader)
	if !bytes.Equal(got, content) {
		t.Errorf("Content mismatch")
	}
}

func TestRawBlobStore_OverflowCheckBeforeAllocation(t *testing.T) {
	defer goleak.VerifyNone(t)
	tempDir := t.TempDir()
	devicePath := filepath.Join(tempDir, "fake-device")
	dataDir := filepath.Join(tempDir, "data")

	cfg := common.ConfigurationStorage{Backend: "raw", RawDevicePath: devicePath}
	daemon := &common.Daemon{Data: dataDir}

	store, err := NewRawBlobStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewRawBlobStore failed: %v", err)
	}
	defer store.Close()

	content := []byte("first blob data")
	hashA := "overflowhashA"

	if err := store.PutBlob(hashA, bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob A failed: %v", err)
	}

	store.mu.Lock()
	store.nextOffset = math.MaxInt64 - common.MaxFileSize + 1
	store.mu.Unlock()

	overflowContent := []byte("overflow blob")
	hashB := "overflowhashB"

	err = store.PutBlob(hashB, bytes.NewReader(overflowContent))
	if err == nil {
		t.Fatal("Expected overflow error, got nil")
	}

	var allocExists bool
	store.allocDB.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketRawAlloc)
		if b != nil {
			allocExists = b.Get([]byte(hashB)) != nil
		}
		return nil
	})
	if allocExists {
		t.Fatal("Allocation entry for hashB should NOT exist after overflow error")
	}

	store.mu.Lock()
	store.nextOffset = int64(len(content))
	store.mu.Unlock()

	hashC := "overflowhashC"
	overwriteContent := []byte("third blob data")
	if err := store.PutBlob(hashC, bytes.NewReader(overwriteContent)); err != nil {
		t.Fatalf("PutBlob C failed: %v", err)
	}

	reader, err := store.GetBlob(hashA)
	if err != nil {
		t.Fatalf("GetBlob A failed: %v", err)
	}
	defer reader.Close()
	got, _ := io.ReadAll(reader)
	if !bytes.Equal(got, content) {
		t.Fatalf("Data corruption: blob A content changed after overflow scenario: got %q, want %q", got, content)
	}
}

func TestRawBlobStore_PathTraversal(t *testing.T) {
	defer goleak.VerifyNone(t)
	tempDir := t.TempDir()
	devicePath := filepath.Join(tempDir, "fake-device")
	dataDir := filepath.Join(tempDir, "data")

	cfg := common.ConfigurationStorage{Backend: "raw", RawDevicePath: devicePath}
	daemon := &common.Daemon{Data: dataDir}

	store, err := NewRawBlobStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewRawBlobStore failed: %v", err)
	}
	defer store.Close()

	badHash := "../../../etc/passwd"

	// Test PutBlob path traversal
	if err := store.PutBlob(badHash, bytes.NewReader([]byte("test"))); err == nil {
		t.Errorf("PutBlob with path traversal should fail")
	}

	// Test GetBlob path traversal
	if _, err := store.GetBlob(badHash); err == nil {
		t.Errorf("GetBlob with path traversal should fail")
	}

	// Test DeleteBlob path traversal
	if err := store.DeleteBlob(badHash); err == nil {
		t.Errorf("DeleteBlob with path traversal should fail")
	}
}

func TestRawBlobStore_GetBlobStreaming(t *testing.T) {
	defer goleak.VerifyNone(t)
	tempDir := t.TempDir()
	devicePath := filepath.Join(tempDir, "fake-device")
	dataDir := filepath.Join(tempDir, "data")

	cfg := common.ConfigurationStorage{Backend: "raw", RawDevicePath: devicePath}
	daemon := &common.Daemon{Data: dataDir}

	store, err := NewRawBlobStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewRawBlobStore failed: %v", err)
	}
	defer store.Close()

	content := []byte("streaming blob payload")
	hash := "streamhash"

	if err := store.PutBlob(hash, bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	reader, err := store.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob failed: %v", err)
	}
	defer reader.Close()

	// GetBlob must return a lazily-read *io.SectionReader, not a fully-
	// buffered byte slice. This prevents OOM for large blobs (issue #589).
	// The SectionReader is wrapped by io.NopCloser, so drill through the
	// embedded io.Reader field to confirm the streaming type.
	rv := reflect.ValueOf(reader)
	if rv.Kind() != reflect.Struct || rv.NumField() == 0 {
		t.Fatalf("GetBlob returned %T, want an io.NopCloser-wrapped *io.SectionReader", reader)
	}
	inner := rv.Field(0).Interface()
	if _, ok := inner.(*io.SectionReader); !ok {
		t.Fatalf("GetBlob reader underlying type is %T, want *io.SectionReader (streaming)", inner)
	}

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read streamed blob: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("Streamed content mismatch: got %q, want %q", got, content)
	}
}
