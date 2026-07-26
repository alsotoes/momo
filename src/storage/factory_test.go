package storage

import (
	"os"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

func TestNewStore_LocalDefault(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-factory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := common.ConfigurationStorage{Backend: "local", GCInterval: 300, TombstoneRetention: 86400}
	daemon := &common.Daemon{Data: tmpDir}

	store, err := NewStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	if err := store.Put("test.txt", "hash123", 5, "", nil); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
}

func TestNewStore_EmptyBackendDefaultsToLocal(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-factory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := common.ConfigurationStorage{Backend: "", GCInterval: 300, TombstoneRetention: 86400}
	daemon := &common.Daemon{Data: tmpDir}

	store, err := NewStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()
}

func TestNewStore_NFS(t *testing.T) {
	defer goleak.VerifyNone(t)
	tmpDir, err := os.MkdirTemp("", "momo-factory-nfs-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := common.ConfigurationStorage{Backend: "nfs", GCInterval: 300, TombstoneRetention: 86400}
	daemon := &common.Daemon{Data: tmpDir}

	store, err := NewStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()
}

func TestNewStore_S3NotYetImplemented(t *testing.T) {
	cfg := common.ConfigurationStorage{Backend: "s3"}
	daemon := &common.Daemon{Data: "/tmp"}

	_, err := NewStore(cfg, daemon)
	if err == nil {
		t.Fatalf("Expected error for s3 backend (not yet implemented)")
	}
}

func TestNewStore_RawNotYetImplemented(t *testing.T) {
	cfg := common.ConfigurationStorage{Backend: "raw"}
	daemon := &common.Daemon{Data: "/tmp"}

	_, err := NewStore(cfg, daemon)
	if err == nil {
		t.Fatalf("Expected error for raw backend (not yet implemented)")
	}
}

func TestNewStore_UnsupportedBackend(t *testing.T) {
	cfg := common.ConfigurationStorage{Backend: "quantum"}
	daemon := &common.Daemon{Data: "/tmp"}

	_, err := NewStore(cfg, daemon)
	if err == nil {
		t.Fatalf("Expected error for unsupported backend")
	}
}
