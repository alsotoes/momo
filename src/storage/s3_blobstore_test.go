package storage

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

// mockS3Server creates an in-process HTTP server that simulates a minimal
// S3-compatible API for testing. It stores blobs in memory keyed by object path.
func mockS3Server(t *testing.T) (*httptest.Server, *sync.Map) {
	t.Helper()
	store := &sync.Map{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/")
		parts := strings.SplitN(key, "/", 2)
		if len(parts) < 2 {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		objectKey := parts[1]

		switch r.Method {
		case http.MethodPut:
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "read error", http.StatusInternalServerError)
				return
			}
			store.Store(objectKey, data)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			val, ok := store.Load(objectKey)
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Write(val.([]byte))
		case http.MethodDelete:
			store.Delete(objectKey)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	server := httptest.NewServer(handler)
	return server, store
}

func TestS3BlobStore_PutGetDelete(t *testing.T) {
	defer goleak.VerifyNone(t)
	server, _ := mockS3Server(t)
	defer server.Close()

	cfg := common.ConfigurationStorage{
		Backend:     "s3",
		S3Endpoint:  server.URL,
		S3Region:    "us-east-1",
		S3Bucket:    "test-bucket",
		S3AccessKey: "AKIAIOSFODNN7EXAMPLE",                     // notsecret
		S3SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", // notsecret
		S3PathStyle: true,
	}

	store, err := NewS3BlobStore(cfg)
	if err != nil {
		t.Fatalf("NewS3BlobStore failed: %v", err)
	}
	defer store.Close()

	content := []byte("hello s3 world")
	hash := "abc123def456"

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
		t.Fatalf("Failed to read blob: %v", err)
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

func TestS3BlobStore_DeleteMissing(t *testing.T) {
	defer goleak.VerifyNone(t)
	server, _ := mockS3Server(t)
	defer server.Close()

	cfg := common.ConfigurationStorage{
		Backend:     "s3",
		S3Endpoint:  server.URL,
		S3Region:    "us-east-1",
		S3Bucket:    "test-bucket",
		S3AccessKey: "AKIAIOSFODNN7EXAMPLE",                     // notsecret
		S3SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", // notsecret
		S3PathStyle: true,
	}

	store, err := NewS3BlobStore(cfg)
	if err != nil {
		t.Fatalf("NewS3BlobStore failed: %v", err)
	}
	defer store.Close()

	// Delete non-existent blob should be a no-op
	if err := store.DeleteBlob("nonexistent"); err != nil {
		t.Errorf("DeleteBlob on missing blob should be nil, got: %v", err)
	}
}

func TestS3BlobStore_GetMissing(t *testing.T) {
	defer goleak.VerifyNone(t)
	server, _ := mockS3Server(t)
	defer server.Close()

	cfg := common.ConfigurationStorage{
		Backend:     "s3",
		S3Endpoint:  server.URL,
		S3Region:    "us-east-1",
		S3Bucket:    "test-bucket",
		S3AccessKey: "AKIAIOSFODNN7EXAMPLE",                     // notsecret
		S3SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", // notsecret
		S3PathStyle: true,
	}

	store, err := NewS3BlobStore(cfg)
	if err != nil {
		t.Fatalf("NewS3BlobStore failed: %v", err)
	}
	defer store.Close()

	_, err = store.GetBlob("nonexistent")
	if err == nil {
		t.Errorf("Expected error for missing blob")
	}
}

func TestS3BlobStore_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		cfg  common.ConfigurationStorage
	}{
		{"missing endpoint", common.ConfigurationStorage{Backend: "s3", S3Bucket: "b", S3AccessKey: "a", S3SecretKey: "s"}},
		{"missing bucket", common.ConfigurationStorage{Backend: "s3", S3Endpoint: "http://x", S3AccessKey: "a", S3SecretKey: "s"}},
		{"missing access key", common.ConfigurationStorage{Backend: "s3", S3Endpoint: "http://x", S3Bucket: "b", S3SecretKey: "s"}},
		{"missing secret key", common.ConfigurationStorage{Backend: "s3", S3Endpoint: "http://x", S3Bucket: "b", S3AccessKey: "a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewS3BlobStore(tt.cfg)
			if err == nil {
				t.Errorf("Expected validation error")
			}
		})
	}
}

func TestS3BlobStore_DefaultRegion(t *testing.T) {
	server, _ := mockS3Server(t)
	defer server.Close()

	cfg := common.ConfigurationStorage{
		Backend:     "s3",
		S3Endpoint:  server.URL,
		S3Bucket:    "test-bucket",
		S3AccessKey: "AKIAIOSFODNN7EXAMPLE",                     // notsecret
		S3SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", // notsecret
		S3PathStyle: true,
	}

	store, err := NewS3BlobStore(cfg)
	if err != nil {
		t.Fatalf("NewS3BlobStore failed: %v", err)
	}
	defer store.Close()

	if store.region != "us-east-1" {
		t.Errorf("Expected default region us-east-1, got %s", store.region)
	}
}

func TestNewStore_S3Backend(t *testing.T) {
	defer goleak.VerifyNone(t)
	server, _ := mockS3Server(t)
	defer server.Close()

	tmpDir, err := os.MkdirTemp("", "momo-s3-factory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := common.ConfigurationStorage{
		Backend:            "s3",
		S3Endpoint:         server.URL,
		S3Region:           "us-east-1",
		S3Bucket:           "test-bucket",
		S3AccessKey:        "AKIAIOSFODNN7EXAMPLE",                     // notsecret
		S3SecretKey:        "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", // notsecret
		S3PathStyle:        true,
		GCInterval:         300,
		TombstoneRetention: 86400,
	}
	daemon := &common.Daemon{Data: tmpDir}

	store, err := NewStore(cfg, daemon)
	if err != nil {
		t.Fatalf("NewStore with s3 backend failed: %v", err)
	}
	defer store.Close()

	content := []byte("test via factory")
	if err := store.Put("file.txt", "hash123", int64(len(content)), "", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	reader, meta, err := store.Get("file.txt")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer reader.Close()

	if meta.Hash != "hash123" {
		t.Errorf("Expected hash hash123, got %s", meta.Hash)
	}

	got, _ := io.ReadAll(reader)
	if !bytes.Equal(got, content) {
		t.Errorf("Content mismatch")
	}
}

func TestNewRequest_PathTraversal(t *testing.T) {
	server, _ := mockS3Server(t)
	defer server.Close()

	cfg := common.ConfigurationStorage{
		Backend:     "s3",
		S3Endpoint:  server.URL,
		S3Bucket:    "test-bucket",
		S3AccessKey: "test", // notsecret
		S3SecretKey: "test", // notsecret
	}

	store, err := NewS3BlobStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// newRequest is internal, but GetBlob calls it with the hash/key
	_, err = store.GetBlob("../etc/passwd")
	if err == nil {
		t.Errorf("Expected path traversal error, got nil")
	} else if !strings.Contains(err.Error(), "invalid key path traversal") {
		t.Errorf("Expected path traversal error, got %v", err)
	}
}
