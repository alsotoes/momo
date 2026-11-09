package storage

import (
	"bytes"
	"testing"
	"time"
)

func newHintStore(t *testing.T) *CASStore {
	t.Helper()
	s, err := NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMetadataHintsRoundTrip(t *testing.T) {
	s := newHintStore(t)
	now := time.Now()
	if err := s.PutMetadataHint(2, "a.txt", []byte("record-a"), now); err != nil {
		t.Fatalf("PutMetadataHint: %v", err)
	}
	if err := s.PutMetadataHint(2, "b.txt", []byte("record-b"), now.Add(time.Second)); err != nil {
		t.Fatalf("PutMetadataHint: %v", err)
	}
	if err := s.PutMetadataHint(3, "c.txt", []byte("record-c"), now); err != nil {
		t.Fatalf("PutMetadataHint: %v", err)
	}

	hints, err := s.ListMetadataHints(2)
	if err != nil {
		t.Fatalf("ListMetadataHints: %v", err)
	}
	if len(hints) != 2 {
		t.Fatalf("peer 2 hints = %d, want 2", len(hints))
	}
	if hints[0].Name != "a.txt" || !bytes.Equal(hints[0].Payload, []byte("record-a")) {
		t.Fatalf("first hint = %+v", hints[0])
	}
	if !hints[0].CreatedAt.Equal(now) {
		t.Fatalf("createdAt = %v, want %v", hints[0].CreatedAt, now)
	}
}

func TestMetadataHintsReplaceDeleteAndIsolation(t *testing.T) {
	s := newHintStore(t)
	now := time.Now()
	if err := s.PutMetadataHint(2, "a.txt", []byte("record-a"), now); err != nil {
		t.Fatalf("PutMetadataHint: %v", err)
	}
	if err := s.PutMetadataHint(2, "b.txt", []byte("record-b"), now); err != nil {
		t.Fatalf("PutMetadataHint: %v", err)
	}
	if err := s.PutMetadataHint(3, "c.txt", []byte("record-c"), now); err != nil {
		t.Fatalf("PutMetadataHint: %v", err)
	}

	// Last write for the same name replaces the earlier payload.
	if err := s.PutMetadataHint(2, "a.txt", []byte("record-a2"), now.Add(2*time.Second)); err != nil {
		t.Fatalf("PutMetadataHint replace: %v", err)
	}
	hints, _ := s.ListMetadataHints(2)
	if len(hints) != 2 || !bytes.Equal(hints[0].Payload, []byte("record-a2")) {
		t.Fatalf("replace failed: %+v", hints)
	}

	if err := s.DeleteMetadataHint(2, "a.txt"); err != nil {
		t.Fatalf("DeleteMetadataHint: %v", err)
	}
	hints, _ = s.ListMetadataHints(2)
	if len(hints) != 1 || hints[0].Name != "b.txt" {
		t.Fatalf("after delete: %+v", hints)
	}
	// Peer isolation: deleting for 2 must not touch 3.
	if other, _ := s.ListMetadataHints(3); len(other) != 1 {
		t.Fatalf("peer 3 hints = %d, want 1", len(other))
	}
}

func TestMetadataHintValidation(t *testing.T) {
	s := newHintStore(t)
	if err := s.PutMetadataHint(1, "", []byte("x"), time.Now()); err == nil {
		t.Fatal("expected an error for an empty name")
	}
	if err := s.PutMetadataHint(1, "a", nil, time.Now()); err == nil {
		t.Fatal("expected an error for an empty payload")
	}
}

func TestPruneMetadataHintsDropsExpired(t *testing.T) {
	s := newHintStore(t)
	now := time.Now()
	if err := s.PutMetadataHint(1, "old", []byte("x"), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("seed old: %v", err)
	}
	if err := s.PutMetadataHint(1, "new", []byte("y"), now); err != nil {
		t.Fatalf("seed new: %v", err)
	}
	pruned, err := s.PruneMetadataHints(now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("PruneMetadataHints: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned = %d, want 1", pruned)
	}
	hints, _ := s.ListMetadataHints(1)
	if len(hints) != 1 || hints[0].Name != "new" {
		t.Fatalf("after prune: %+v", hints)
	}
}
