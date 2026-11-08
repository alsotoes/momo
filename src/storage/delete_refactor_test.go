package storage

import (
	"bytes"
	"testing"

	"github.com/alsotoes/momo/src/common"
)

// TestDeleteRefCountAndOrphanEviction pins the delete path after it was split
// into deleteTx/decrementRefCount/evictOrphanedBlob (#1142).
func TestDeleteRefCountAndOrphanEviction(t *testing.T) {
	s, err := NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	payload := []byte("shared payload")
	hash := putTestBlob(t, s, "a", payload)
	// Second name referencing the same hash: dedup keeps one blob.
	if err := s.Put("b", hash, int64(len(payload)), "", bytes.NewReader(payload)); err != nil {
		t.Fatalf("Put b: %v", err)
	}

	if err := s.Delete("a"); err != nil {
		t.Fatalf("Delete a: %v", err)
	}
	if _, err := s.GetBlobByHash(hash); err != nil {
		t.Fatalf("blob must survive while b references it: %v", err)
	}

	if err := s.Delete("b"); err != nil {
		t.Fatalf("Delete b: %v", err)
	}
	if _, err := s.GetBlobByHash(hash); err == nil {
		t.Fatal("the orphaned blob must be evicted with the last reference")
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected an empty listing, got %v", list)
	}
}

// TestDeleteMissingNameWritesTombstone covers the hashBytes == nil branch.
func TestDeleteMissingNameWritesTombstone(t *testing.T) {
	s, err := NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Delete("never-existed"); err != nil {
		t.Fatalf("deleting a missing name must succeed: %v", err)
	}
}

// putTestBlob stores content under name and returns its hash.
func putTestBlob(t *testing.T, s *CASStore, name string, payload []byte) string {
	t.Helper()
	hash := common.HashBytes(payload)
	if err := s.Put(name, hash, int64(len(payload)), "", bytes.NewReader(payload)); err != nil {
		t.Fatalf("Put %s: %v", name, err)
	}
	return hash
}
