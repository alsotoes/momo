package storage

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"

	"github.com/alsotoes/momo/src/common"
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

	store, err := NewStore(cfg, daemon)
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
