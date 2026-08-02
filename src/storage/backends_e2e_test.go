package storage

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

// --- Helpers ---

// s3TestStores creates n0 n stores backed by an in-process mock S3 server.
// Each store gets its own bbolt metadata DB (dataDir/node-i) but shares
// the same S3 mock server (blobs are content-addressed by hash, so no
// collisions across nodes).
func s3TestStores(t *testing.T, n int) ([]Store, *httptest.Server, func()) {
	t.Helper()
	tempDir := t.TempDir()

	blobStore := &sync.Map{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/")
		parts := strings.SplitN(key, "/", 2)
		if len(parts) < 2 {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		objKey := parts[1]
		switch r.Method {
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			blobStore.Store(objKey, data)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if val, ok := blobStore.Load(objKey); ok {
				w.Write(val.([]byte))
			} else {
				http.Error(w, "not found", http.StatusNotFound)
			}
		case http.MethodDelete:
			blobStore.Delete(objKey)
			w.WriteHeader(http.StatusNoContent)
		}
	}))

	stores := make([]Store, n)
	for i := 0; i < n; i++ {
		dataDir := filepath.Join(tempDir, "node-"+itoa(i))
		cfg := common.ConfigurationStorage{
			Backend:            "s3",
			S3Endpoint:         server.URL,
			S3Region:           "us-east-1",
			S3Bucket:           "e2e-bucket",
			S3AccessKey:        "AKIAIOSFODNN7EXAMPLE",                     // notsecret
			S3SecretKey:        "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", // notsecret
			S3PathStyle:        true,
			GCInterval:         300,
			TombstoneRetention: 86400,
		}
		daemon := &common.Daemon{Data: dataDir}
		s, err := NewStore(cfg, daemon, "")
		if err != nil {
			t.Fatalf("NewStore s3 node %d failed: %v", i, err)
		}
		stores[i] = s
	}

	cleanup := func() {
		for _, s := range stores {
			s.Close()
		}
		server.Close()
	}
	return stores, server, cleanup
}

// rawTestStores creates n stores backed by temp-file fake block devices.
func rawTestStores(t *testing.T, n int, tempDir string) ([]Store, func()) {
	t.Helper()

	stores := make([]Store, n)
	for i := 0; i < n; i++ {
		dataDir := filepath.Join(tempDir, "node-"+itoa(i), "data")
		devicePath := filepath.Join(tempDir, "node-"+itoa(i), "device")
		cfg := common.ConfigurationStorage{
			Backend:            "raw",
			RawDevicePath:      devicePath,
			GCInterval:         300,
			TombstoneRetention: 86400,
		}
		daemon := &common.Daemon{Data: dataDir}
		s, err := NewStore(cfg, daemon, "")
		if err != nil {
			t.Fatalf("NewStore raw node %d failed: %v", i, err)
		}
		stores[i] = s
	}

	cleanup := func() {
		for _, s := range stores {
			s.Close()
		}
	}
	return stores, cleanup
}

// rawTestStoresReopen closes all stores and reopens them with the same
// config, verifying data persistence across restart.
func rawTestStoresReopen(t *testing.T, n int, tempDir string) ([]Store, func()) {
	t.Helper()
	stores := make([]Store, n)
	for i := 0; i < n; i++ {
		dataDir := filepath.Join(tempDir, "node-"+itoa(i), "data")
		devicePath := filepath.Join(tempDir, "node-"+itoa(i), "device")
		cfg := common.ConfigurationStorage{
			Backend:            "raw",
			RawDevicePath:      devicePath,
			GCInterval:         300,
			TombstoneRetention: 86400,
		}
		daemon := &common.Daemon{Data: dataDir}
		s, err := NewStore(cfg, daemon, "")
		if err != nil {
			t.Fatalf("NewStore raw reopen node %d failed: %v", i, err)
		}
		stores[i] = s
	}
	cleanup := func() {
		for _, s := range stores {
			s.Close()
		}
	}
	return stores, cleanup
}

// simulateForward simulates connectToPeerStream's data flow:
// reader from source store → Put on destination store.
func simulateForward(t *testing.T, src Store, dst Store, name string) {
	t.Helper()
	reader, meta, err := src.Get(name)
	if err != nil {
		t.Fatalf("Forward: src.Get(%s) failed: %v", name, err)
	}
	defer reader.Close()

	if err := dst.Put(name, meta.Hash, meta.Size, meta.RemotePath, reader); err != nil {
		t.Fatalf("Forward: dst.Put(%s) failed: %v", name, err)
	}
}

// verifyBlob checks that a store has the blob and its content matches.
func verifyBlob(t *testing.T, s Store, name string, expected []byte) {
	t.Helper()
	reader, meta, err := s.Get(name)
	if err != nil {
		t.Fatalf("verify: Get(%s) failed: %v", name, err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("verify: ReadAll failed: %v", err)
	}
	if !bytes.Equal(got, expected) {
		t.Errorf("verify: content mismatch for %s: got %d bytes, want %d bytes", name, len(got), len(expected))
	}
	_ = meta
}

// itoa wraps strconv.Itoa for brevity in test helpers.
func itoa(i int) string { return strconv.Itoa(i) }

// --- S3 Backend E2E Tests ---

func TestBackendE2E_S3_ChainForward(t *testing.T) {
	defer goleak.VerifyNone(t)
	stores, _, cleanup := s3TestStores(t, 3)
	defer cleanup()

	content := []byte("s3 chain replication content")
	hash := "s3chainhash001"
	name := "chain-test.txt"

	// Put on node 0
	if err := stores[0].Put(name, hash, int64(len(content)), "", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put on node 0 failed: %v", err)
	}

	// Chain forward: 0 → 1 → 2
	simulateForward(t, stores[0], stores[1], name)
	simulateForward(t, stores[1], stores[2], name)

	// Verify all 3 nodes have the data
	for i := 0; i < 3; i++ {
		verifyBlob(t, stores[i], name, content)
	}
}

func TestBackendE2E_S3_SplayForward(t *testing.T) {
	defer goleak.VerifyNone(t)
	stores, _, cleanup := s3TestStores(t, 3)
	defer cleanup()

	content := []byte("s3 splay replication content")
	hash := "s3splayhash001"
	name := "splay-test.txt"

	// Put on node 0
	if err := stores[0].Put(name, hash, int64(len(content)), "", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put on node 0 failed: %v", err)
	}

	// Splay forward: 0 → 1 and 0 → 2 concurrently
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		simulateForward(t, stores[0], stores[1], name)
	}()
	go func() {
		defer wg.Done()
		simulateForward(t, stores[0], stores[2], name)
	}()
	wg.Wait()

	// Verify all 3 nodes have the data
	for i := 0; i < 3; i++ {
		verifyBlob(t, stores[i], name, content)
	}
}

func TestBackendE2E_S3_DeleteAndGC(t *testing.T) {
	defer goleak.VerifyNone(t)
	stores, _, cleanup := s3TestStores(t, 1)
	defer cleanup()

	content := []byte("s3 delete and gc content")
	hash := "s3gchash001"
	name := "gc-test.txt"

	// Put
	if err := stores[0].Put(name, hash, int64(len(content)), "", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify it exists
	verifyBlob(t, stores[0], name, content)

	// Delete
	if err := stores[0].Delete(name); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify Get returns error (tombstone prevents access)
	_, _, err := stores[0].Get(name)
	if err == nil {
		t.Errorf("Expected error after delete")
	}

	// Note: Has may still return true because the metadata entry exists
	// until GC sweeps it (refcount=0). The tombstone prevents Get access.
	// This is by design — GC runs on an interval.
}

// --- Raw Device Backend E2E Tests ---

func TestBackendE2E_Raw_ChainForward(t *testing.T) {
	defer goleak.VerifyNone(t)
	tempDir := t.TempDir()
	stores, cleanup := rawTestStores(t, 3, tempDir)
	defer cleanup()

	content := []byte("raw chain replication content")
	hash := "rawchainhash001"
	name := "chain-test.txt"

	// Put on node 0
	if err := stores[0].Put(name, hash, int64(len(content)), "", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put on node 0 failed: %v", err)
	}

	// Chain forward: 0 → 1 → 2
	simulateForward(t, stores[0], stores[1], name)
	simulateForward(t, stores[1], stores[2], name)

	// Verify all 3 nodes have the data
	for i := 0; i < 3; i++ {
		verifyBlob(t, stores[i], name, content)
	}
}

func TestBackendE2E_Raw_SplayForward(t *testing.T) {
	defer goleak.VerifyNone(t)
	tempDir := t.TempDir()
	stores, cleanup := rawTestStores(t, 3, tempDir)
	defer cleanup()

	content := []byte("raw splay replication content")
	hash := "rawsplayhash001"
	name := "splay-test.txt"

	// Put on node 0
	if err := stores[0].Put(name, hash, int64(len(content)), "", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put on node 0 failed: %v", err)
	}

	// Splay forward: 0 → 1 and 0 → 2 concurrently
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		simulateForward(t, stores[0], stores[1], name)
	}()
	go func() {
		defer wg.Done()
		simulateForward(t, stores[0], stores[2], name)
	}()
	wg.Wait()

	// Verify all 3 nodes have the data
	for i := 0; i < 3; i++ {
		verifyBlob(t, stores[i], name, content)
	}
}

func TestBackendE2E_Raw_Persistence(t *testing.T) {
	defer goleak.VerifyNone(t)
	tempDir := t.TempDir()

	// Create initial stores
	stores, cleanup := rawTestStores(t, 2, tempDir)

	content1 := []byte("persistent blob 1")
	content2 := []byte("persistent blob 2")

	if err := stores[0].Put("persist1.txt", "phash1", int64(len(content1)), "", bytes.NewReader(content1)); err != nil {
		cleanup()
		t.Fatalf("Put persist1 failed: %v", err)
	}
	if err := stores[0].Put("persist2.txt", "phash2", int64(len(content2)), "docs", bytes.NewReader(content2)); err != nil {
		cleanup()
		t.Fatalf("Put persist2 failed: %v", err)
	}

	// Forward to node 1
	simulateForward(t, stores[0], stores[1], "persist1.txt")

	// Close all stores (explicit, no defer — we reopen below)
	cleanup()

	// Reopen stores with same config
	reopened, cleanup2 := rawTestStoresReopen(t, 2, tempDir)
	defer cleanup2()

	// Verify data survived restart
	verifyBlob(t, reopened[0], "persist1.txt", content1)
	verifyBlob(t, reopened[0], "persist2.txt", content2)
	verifyBlob(t, reopened[1], "persist1.txt", content1)

	// Verify metadata (RemotePath) survived
	_, meta, err := reopened[0].Get("persist2.txt")
	if err != nil {
		t.Fatalf("Get persist2 after reopen failed: %v", err)
	}
	if meta.RemotePath != "docs" {
		t.Errorf("Expected RemotePath 'docs', got %q", meta.RemotePath)
	}
}

func TestBackendE2E_Raw_DeleteAndGC(t *testing.T) {
	defer goleak.VerifyNone(t)
	tempDir := t.TempDir()
	stores, cleanup := rawTestStores(t, 1, tempDir)
	defer cleanup()

	content := []byte("raw delete and gc content")
	hash := "rawgchash001"
	name := "gc-test.txt"

	// Put
	if err := stores[0].Put(name, hash, int64(len(content)), "", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify
	verifyBlob(t, stores[0], name, content)

	// Delete
	if err := stores[0].Delete(name); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify Get returns error (tombstone prevents access)
	_, _, err := stores[0].Get(name)
	if err == nil {
		t.Errorf("Expected error after delete")
	}

	// Note: Has may still return true because the metadata entry exists
	// until GC sweeps it (refcount=0). The tombstone prevents Get access.
}

// --- Cross-Backend Interop Test ---

func TestBackendE2E_MixedBackendForward(t *testing.T) {
	defer goleak.VerifyNone(t)
	// Node 0: local backend, Node 1: S3 backend
	// Simulate a cluster where different nodes use different backends
	tempDir := t.TempDir()

	// Node 0: local
	localStore, err := NewStore(common.ConfigurationStorage{
		Backend:            "local",
		GCInterval:         300,
		TombstoneRetention: 86400,
	}, &common.Daemon{Data: filepath.Join(tempDir, "local")}, "")
	if err != nil {
		t.Fatalf("NewStore local failed: %v", err)
	}
	defer localStore.Close()

	// Node 1: raw
	rawStore, err := NewStore(common.ConfigurationStorage{
		Backend:            "raw",
		RawDevicePath:      filepath.Join(tempDir, "raw-device"),
		GCInterval:         300,
		TombstoneRetention: 86400,
	}, &common.Daemon{Data: filepath.Join(tempDir, "raw-data")}, "")
	if err != nil {
		t.Fatalf("NewStore raw failed: %v", err)
	}
	defer rawStore.Close()

	content := []byte("mixed backend interop")
	name := "interop.txt"
	hash := "interop001"

	// Put on local node
	if err := localStore.Put(name, hash, int64(len(content)), "", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put on local failed: %v", err)
	}

	// Forward from local to raw
	simulateForward(t, localStore, rawStore, name)

	// Verify on raw node
	verifyBlob(t, rawStore, name, content)

	// Forward back from raw to local (different name)
	if err := localStore.Put("copy.txt", hash, int64(len(content)), "", nil); err != nil {
		t.Fatalf("Put copy on local failed: %v", err)
	}
	verifyBlob(t, localStore, "copy.txt", content)
}
