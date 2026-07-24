package storage

import (
	"bytes"
	"os"
	"testing"
	"time"

	"go.etcd.io/bbolt"
	"go.uber.org/goleak"
)

func TestRefcountDedupDelete(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-gc-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	content := []byte("shared content")
	hash := "aaa111bbb222ccc333ddd444eee555f"
	store.Put("file_a.txt", hash, int64(len(content)), "", bytes.NewReader(content))
	store.Put("file_b.txt", hash, int64(len(content)), "", bytes.NewReader(content))

	// Both names should resolve
	rc, _, err := store.Get("file_a.txt")
	if err != nil {
		t.Fatalf("Get file_a.txt failed: %v", err)
	}
	rc.Close()

	rc, _, err = store.Get("file_b.txt")
	if err != nil {
		t.Fatalf("Get file_b.txt failed: %v", err)
	}
	rc.Close()

	// Delete one name — the other should still work
	if err := store.Delete("file_a.txt"); err != nil {
		t.Fatalf("Delete file_a.txt failed: %v", err)
	}

	// file_a should be gone
	_, _, err = store.Get("file_a.txt")
	if err == nil {
		t.Fatal("Expected error getting deleted file_a.txt")
	}

	// file_b should still work
	rc, _, err = store.Get("file_b.txt")
	if err != nil {
		t.Fatalf("Get file_b.txt after deleting file_a.txt failed: %v", err)
	}
	rc.Close()

	// Blob file should still exist on disk
	blobPath := store.getBlobPath(hash)
	if _, err := os.Stat(blobPath); os.IsNotExist(err) {
		t.Fatal("Blob file removed while refcount > 0")
	}
}

func TestTombstoneHidesFromListAndGet(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-gc-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	content := []byte("tombstoned content")
	hash := "bbb222ccc333ddd444eee555f666777"
	store.Put("todelete.txt", hash, int64(len(content)), "", bytes.NewReader(content))

	if err := store.Delete("todelete.txt"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// List should not include the deleted file
	list, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	for _, f := range list {
		if f.Name == "todelete.txt" {
			t.Fatal("Deleted file appeared in List")
		}
	}

	// Get should return ENOENT
	_, _, err = store.Get("todelete.txt")
	if err == nil {
		t.Fatal("Expected error getting tombstoned file")
	}

	// GetBlobPath should also return ENOENT
	_, err = store.GetBlobPath("todelete.txt")
	if err == nil {
		t.Fatal("Expected error getting blob path for tombstoned file")
	}
}

func TestGCSweeperRemovesOrphanedBlobs(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-gc-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	content := []byte("gc me")
	hash := "ccc333ddd444eee555f666777888aaa"
	store.Put("gc_target.txt", hash, int64(len(content)), "", bytes.NewReader(content))

	blobPath := store.getBlobPath(hash)
	if _, err := os.Stat(blobPath); os.IsNotExist(err) {
		t.Fatal("Blob file should exist before GC")
	}

	// Delete the only name — refcount drops to 0
	if err := store.Delete("gc_target.txt"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Run GC sweep manually
	if err := store.sweepOrphanedBlobs(); err != nil {
		t.Fatalf("sweepOrphanedBlobs failed: %v", err)
	}

	// Blob file should be gone
	if _, err := os.Stat(blobPath); !os.IsNotExist(err) {
		t.Fatal("Blob file should be removed by GC")
	}

	// Has should return false
	exists, _ := store.Has(hash)
	if exists {
		t.Fatal("Has should return false after GC removed the blob")
	}
}

func TestGCSweeperKeepsReferencedBlobs(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-gc-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	content := []byte("keep me")
	hash := "ddd444eee555f666777888aaa999bbb"
	store.Put("keep.txt", hash, int64(len(content)), "", bytes.NewReader(content))

	// Run GC — nothing should be removed
	if err := store.sweepOrphanedBlobs(); err != nil {
		t.Fatalf("sweepOrphanedBlobs failed: %v", err)
	}

	blobPath := store.getBlobPath(hash)
	if _, err := os.Stat(blobPath); os.IsNotExist(err) {
		t.Fatal("Blob file should still exist — refcount > 0")
	}
}

func TestTombstoneExpiry(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-gc-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	content := []byte("expire me")
	hash := "eee555f666777888aaa999bbbccc111"
	store.Put("expire.txt", hash, int64(len(content)), "", bytes.NewReader(content))
	store.Delete("expire.txt")

	// Tombstone should exist
	ts, err := store.GetTombstones()
	if err != nil {
		t.Fatalf("GetTombstones failed: %v", err)
	}
	if len(ts) != 1 {
		t.Fatalf("Expected 1 tombstone, got %d", len(ts))
	}

	// Expire tombstones with 0 retention (everything expires)
	if err := store.sweepExpiredTombstones(0); err != nil {
		t.Fatalf("sweepExpiredTombstones failed: %v", err)
	}

	ts, err = store.GetTombstones()
	if err != nil {
		t.Fatalf("GetTombstones failed: %v", err)
	}
	if len(ts) != 0 {
		t.Fatalf("Expected 0 tombstones after expiry, got %d", len(ts))
	}
}

func TestResurrection(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-gc-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	content := []byte("resurrect me")
	hash := "fff666777888aaa999bbbccc111222"
	store.Put("resurrect.txt", hash, int64(len(content)), "", bytes.NewReader(content))
	store.Delete("resurrect.txt")

	// Should be tombstoned
	_, _, err = store.Get("resurrect.txt")
	if err == nil {
		t.Fatal("Expected error after delete")
	}

	// Resurrect by putting again
	store.Put("resurrect.txt", hash, int64(len(content)), "", bytes.NewReader(content))

	// Should work again
	rc, _, err := store.Get("resurrect.txt")
	if err != nil {
		t.Fatalf("Get after resurrection failed: %v", err)
	}
	rc.Close()

	// Should appear in List
	list, _ := store.List()
	found := false
	for _, f := range list {
		if f.Name == "resurrect.txt" {
			found = true
		}
	}
	if !found {
		t.Fatal("Resurrected file not found in List")
	}

	// Tombstone should be gone
	ts, _ := store.GetTombstones()
	if len(ts) != 0 {
		t.Fatalf("Expected 0 tombstones after resurrection, got %d", len(ts))
	}
}

func TestApplyTombstoneFromRemote(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-gc-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	content := []byte("remote delete")
	hash := "111777888aaa999bbbccc111222333"
	store.Put("remote.txt", hash, int64(len(content)), "", bytes.NewReader(content))

	// Apply tombstone from a remote peer
	deletedAt := time.Now().UnixNano()
	if err := store.ApplyTombstone("remote.txt", deletedAt); err != nil {
		t.Fatalf("ApplyTombstone failed: %v", err)
	}

	// Should be hidden from List and Get
	_, _, err = store.Get("remote.txt")
	if err == nil {
		t.Fatal("Expected error after applying remote tombstone")
	}

	list, _ := store.List()
	for _, f := range list {
		if f.Name == "remote.txt" {
			t.Fatal("Tombstoned file appeared in List")
		}
	}

	// Tombstone should exist
	ts, _ := store.GetTombstones()
	if len(ts) != 1 {
		t.Fatalf("Expected 1 tombstone, got %d", len(ts))
	}
}

func TestGCBackgroundSweeper(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-gc-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	content := []byte("bg gc")
	hash := "222888aaa999bbbccc111222333444"
	store.Put("bg_gc.txt", hash, int64(len(content)), "", bytes.NewReader(content))
	store.Delete("bg_gc.txt")

	blobPath := store.getBlobPath(hash)

	// Start GC with very short interval
	store.StartGC(GCConfig{
		Interval:           100 * time.Millisecond,
		TombstoneRetention: 1 * time.Hour,
	})

	// Wait for GC to run
	time.Sleep(500 * time.Millisecond)

	// Blob should be gone
	if _, err := os.Stat(blobPath); !os.IsNotExist(err) {
		t.Fatal("Blob file should be removed by background GC")
	}
}

func TestQueryDeleteHandler(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-gc-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	content := []byte("handler delete")
	hash := "333999bbbccc111222333444555666"
	store.Put("handler.txt", hash, int64(len(content)), "", bytes.NewReader(content))

	// Verify it exists
	rc, _, err := store.Get("handler.txt")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	rc.Close()

	// Delete via store.Delete (simulating what QueryDelete handler does)
	if err := store.Delete("handler.txt"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	_, _, err = store.Get("handler.txt")
	if err == nil {
		t.Fatal("Expected error after delete")
	}
}

func TestLegacyObjectMetaCompatibility(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-gc-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewCASStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create CASStore: %v", err)
	}
	defer store.Close()

	// Manually write a legacy-format object (ASCII size, no refcount)
	hash := "444aaa111222333444555666777888"

	store.mu.Lock()
	store.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketObjects).Put([]byte(hash), []byte("6"))
	})
	store.mu.Unlock()

	// Decode should handle legacy format
	meta := decodeObjectMeta([]byte("6"))
	if meta.Size != 6 || meta.RefCount != 1 || meta.DeletedAt != 0 {
		t.Fatalf("Legacy decode wrong: %+v", meta)
	}

	// New format should encode/decode correctly
	newMeta := ObjectMeta{Size: 42, RefCount: 3, DeletedAt: 12345}
	encoded := newMeta.encode()
	decoded := decodeObjectMeta(encoded)
	if decoded != newMeta {
		t.Fatalf("Round-trip failed: %+v != %+v", decoded, newMeta)
	}
}
