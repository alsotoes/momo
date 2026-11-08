package storage

import (
	"errors"
	"syscall"
	"testing"
)

func TestGetMetaResolvesFromReplicaSet(t *testing.T) {
	s, err := NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	calls := 0
	s.SetMetadataResolver(func(name string) (DistributedMeta, error) {
		calls++
		if name != "remote-obj" {
			return DistributedMeta{}, syscall.ENOENT
		}
		return DistributedMeta{Hash: "h-remote", Size: 42, RemotePath: "p", VectorClock: []uint64{3}, ShardKey: "s"}, nil
	})

	meta, err := s.GetMeta("remote-obj")
	if err != nil {
		t.Fatalf("GetMeta with resolver: %v", err)
	}
	if meta.Hash != "h-remote" || meta.Size != 42 {
		t.Fatalf("resolved record mismatch: %+v", meta)
	}
	// Second read is local: the resolved record was applied.
	if _, err := s.GetMeta("remote-obj"); err != nil {
		t.Fatalf("second read: %v", err)
	}
	if calls != 1 {
		t.Fatalf("resolver must run once, ran %d times", calls)
	}
}

func TestGetMetaResolverMissKeepsNotFound(t *testing.T) {
	s, err := NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.SetMetadataResolver(func(string) (DistributedMeta, error) {
		return DistributedMeta{}, syscall.ENOENT
	})
	if _, err := s.GetMeta("nope"); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("want ENOENT, got %v", err)
	}
}

func TestGetMetaResolverErrorIsTolerated(t *testing.T) {
	s, err := NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.SetMetadataResolver(func(string) (DistributedMeta, error) {
		return DistributedMeta{}, errors.New("cluster unreachable")
	})
	if _, err := s.GetMeta("nope"); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("a resolver failure must not mask not-found, got %v", err)
	}
}

func TestGetMetaTombstoneSkipsResolver(t *testing.T) {
	s, err := NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Delete("deleted"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	called := false
	s.SetMetadataResolver(func(string) (DistributedMeta, error) {
		called = true
		return DistributedMeta{}, syscall.ENOENT
	})
	if _, err := s.GetMeta("deleted"); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("want ENOENT, got %v", err)
	}
	if called {
		t.Fatal("a tombstoned name must not be resolved from replicas (it would resurrect)")
	}
}

func TestSetMetadataResolverNilDisables(t *testing.T) {
	s, err := NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.SetMetadataResolver(nil)
	if s.metadataResolver() != nil {
		t.Fatal("nil resolver must disable fallback")
	}
}
