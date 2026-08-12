package storage

import (
	"bytes"
	"crypto/hmac"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestS3BlobStore_NoOverallClientTimeout(t *testing.T) {
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

	if store.client.Timeout != 0 {
		t.Errorf("http.Client.Timeout must be zero (would abort slow large downloads); got %v", store.client.Timeout)
	}
	transport, ok := store.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", store.client.Transport)
	}
	if transport.DialContext == nil || transport.ResponseHeaderTimeout == 0 {
		t.Errorf("expected dial and response-header timeouts to be configured, got DialContext=%v ResponseHeaderTimeout=%v",
			transport.DialContext != nil, transport.ResponseHeaderTimeout)
	}

	// A slow-but-steady streamed download must succeed; previously the overall
	// http.Client.Timeout covered the body read and would abort a slow transfer.
	const totalBytes = 1024 * 1024
	const segment = 16 * 1024
	const segmentCount = totalBytes / segment

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		for i := 0; i < segmentCount; i++ {
			time.Sleep(2 * time.Millisecond)
			if _, err := w.Write(make([]byte, segment)); err != nil {
				return
			}
		}
	}))
	defer server2.Close()

	cfg2 := common.ConfigurationStorage{
		Backend:     "s3",
		S3Endpoint:  server2.URL,
		S3Region:    "us-east-1",
		S3Bucket:    "test-bucket",
		S3AccessKey: "AKIAIOSFODNN7EXAMPLE",                     // notsecret
		S3SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", // notsecret
		S3PathStyle: true,
	}
	store2, err := NewS3BlobStore(cfg2)
	if err != nil {
		t.Fatalf("NewS3BlobStore failed: %v", err)
	}
	defer store2.Close()

	reader, err := store2.GetBlob("slow-blob")
	if err != nil {
		t.Fatalf("GetBlob over slow connection failed: %v", err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed reading slow blob: %v", err)
	}
	if len(got) != totalBytes {
		t.Errorf("size mismatch: got %d, want %d", len(got), totalBytes)
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

	store, err := NewStore(cfg, daemon, "")
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

func TestS3BlobStore_PutLargeBlob(t *testing.T) {
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

	content := bytes.Repeat([]byte("x"), 10*1024*1024)
	hash := "largeblobhash123456"

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
	if len(got) != len(content) {
		t.Errorf("Size mismatch: got %d, want %d", len(got), len(content))
	}
	if !bytes.Equal(got, content) {
		t.Errorf("Content mismatch in large blob")
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

	_, err = store.GetBlob("../etc/passwd")
	if err == nil {
		t.Errorf("Expected path traversal error, got nil")
	} else if !strings.Contains(err.Error(), "invalid key path traversal") {
		t.Errorf("Expected path traversal error, got %v", err)
	}
}

func TestS3BlobStore_VirtualHostedURL(t *testing.T) {
	defer goleak.VerifyNone(t)

	cfg := common.ConfigurationStorage{
		Backend:     "s3",
		S3Endpoint:  "https://s3.amazonaws.com",
		S3Bucket:    "mybucket",
		S3AccessKey: "test", // notsecret
		S3SecretKey: "test", // notsecret
		S3PathStyle: false,
	}

	store, err := NewS3BlobStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	req, err := store.newRequest("GET", "testfile.txt", nil, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	if err != nil {
		t.Fatalf("newRequest failed: %v", err)
	}

	expectedHost := "mybucket.s3.amazonaws.com"
	if req.Host != expectedHost {
		t.Errorf("Expected Host %q, got %q", expectedHost, req.Host)
	}
	if req.URL.Host != expectedHost {
		t.Errorf("Expected URL.Host %q, got %q", expectedHost, req.URL.Host)
	}
	if req.URL.Path != "/testfile.txt" {
		t.Errorf("Expected URL.Path /testfile.txt, got %q", req.URL.Path)
	}
}

func TestS3BlobStore_PathStyleURL(t *testing.T) {
	defer goleak.VerifyNone(t)

	cfg := common.ConfigurationStorage{
		Backend:     "s3",
		S3Endpoint:  "https://s3.amazonaws.com",
		S3Bucket:    "mybucket",
		S3AccessKey: "test", // notsecret
		S3SecretKey: "test", // notsecret
		S3PathStyle: true,
	}

	store, err := NewS3BlobStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	req, err := store.newRequest("GET", "testfile.txt", nil, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	if err != nil {
		t.Fatalf("newRequest failed: %v", err)
	}

	expectedHost := "s3.amazonaws.com"
	if req.Host != expectedHost {
		t.Errorf("Expected Host %q, got %q", expectedHost, req.Host)
	}
	if req.URL.Path != "/mybucket/testfile.txt" {
		t.Errorf("Expected URL.Path /mybucket/testfile.txt, got %q", req.URL.Path)
	}
}

const (
	s3TestAccessKey = "AKIAIOSFODNN7EXAMPLE"                     // notsecret
	s3TestSecretKey = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" // notsecret
)

// verifyingS3Server is an in-process S3-compatible server that enforces
// SIGNED_PAYLOAD semantics (issue #776): unless the declared
// X-Amz-Content-Sha256 is the UNSIGNED-PAYLOAD literal, it must equal the
// SHA-256 of the actual body bytes; and the SigV4 Authorization header must
// verify against the declared hash. Violations are rejected with 403.
func verifyingS3Server(t *testing.T, accessKey, secretKey, region string) (*httptest.Server, *sync.Map) {
	t.Helper()
	store := &sync.Map{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}
		declared := r.Header.Get("X-Amz-Content-Sha256")
		if declared != "UNSIGNED-PAYLOAD" && declared != hexSHA256(body) {
			http.Error(w, "payload hash does not match body", http.StatusForbidden)
			return
		}
		if !verifySigV4(t, r, accessKey, secretKey, region, declared) {
			http.Error(w, "signature mismatch", http.StatusForbidden)
			return
		}

		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
		if len(parts) < 2 {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		objectKey := parts[1]

		switch r.Method {
		case http.MethodPut:
			store.Store(objectKey, body)
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
	}))

	return server, store
}

// verifySigV4 recomputes the SigV4 signature for the request as received
// (using the server-observed Host and declared payload hash) and compares it
// against the Authorization header. It mirrors S3BlobStore.getStringToSign.
func verifySigV4(t *testing.T, r *http.Request, accessKey, secretKey, region, payloadHash string) bool {
	t.Helper()
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		return false
	}
	var authAccessKey, signature string
	for _, part := range strings.Split(strings.TrimPrefix(auth, "AWS4-HMAC-SHA256 "), ", ") {
		switch {
		case strings.HasPrefix(part, "Credential="):
			cred := strings.TrimPrefix(part, "Credential=")
			if i := strings.IndexByte(cred, '/'); i > 0 {
				authAccessKey = cred[:i]
			}
		case strings.HasPrefix(part, "Signature="):
			signature = strings.TrimPrefix(part, "Signature=")
		}
	}
	if authAccessKey != accessKey {
		return false
	}

	amzDate := r.Header.Get("X-Amz-Date")
	if len(amzDate) < 8 {
		return false
	}
	dateStamp := amzDate[:8]

	u := &url.URL{Scheme: r.URL.Scheme, Host: r.Host, Path: r.URL.Path}
	store := &S3BlobStore{region: region, secretKey: secretKey}
	sts, err := store.getStringToSign(r.Method, u, amzDate, dateStamp, payloadHash)
	if err != nil {
		return false
	}
	expected := hexHMAC(store.getSigningKey(dateStamp), sts)
	return hmac.Equal([]byte(expected), []byte(signature))
}

func TestS3BlobStore_PutBlob_UsesRealPayloadHash(t *testing.T) {
	defer goleak.VerifyNone(t)

	var mu sync.Mutex
	var declared string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		declared = r.Header.Get("X-Amz-Content-Sha256")
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := common.ConfigurationStorage{
		Backend:     "s3",
		S3Endpoint:  server.URL,
		S3Region:    "us-east-1",
		S3Bucket:    "test-bucket",
		S3AccessKey: s3TestAccessKey,
		S3SecretKey: s3TestSecretKey,
		S3PathStyle: true,
	}
	store, err := NewS3BlobStore(cfg)
	if err != nil {
		t.Fatalf("NewS3BlobStore failed: %v", err)
	}
	defer store.Close()

	content := []byte("check me")
	if err := store.PutBlob("hash123", bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if declared == "" || declared == "UNSIGNED-PAYLOAD" {
		t.Errorf("PutBlob must sign with SIGNED_PAYLOAD (real SHA-256), got declared hash %q", declared)
	}
	if declared != hexSHA256(content) {
		t.Errorf("declared payload hash = %q, want %q", declared, hexSHA256(content))
	}
}

func TestS3BlobStore_PutBlob_SignedPayload(t *testing.T) {
	defer goleak.VerifyNone(t)

	server, _ := verifyingS3Server(t, s3TestAccessKey, s3TestSecretKey, "us-east-1")
	defer server.Close()

	cfg := common.ConfigurationStorage{
		Backend:     "s3",
		S3Endpoint:  server.URL,
		S3Region:    "us-east-1",
		S3Bucket:    "test-bucket",
		S3AccessKey: s3TestAccessKey,
		S3SecretKey: s3TestSecretKey,
		S3PathStyle: true,
	}
	store, err := NewS3BlobStore(cfg)
	if err != nil {
		t.Fatalf("NewS3BlobStore failed: %v", err)
	}
	defer store.Close()

	content := []byte("signed payload content")
	if err := store.PutBlob("hash123", bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob with SIGNED_PAYLOAD failed against verifying server: %v", err)
	}

	reader, err := store.GetBlob("hash123")
	if err != nil {
		t.Fatalf("GetBlob failed: %v", err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed reading blob: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestS3BlobStore_SignatureBindsContent(t *testing.T) {
	defer goleak.VerifyNone(t)

	server, _ := verifyingS3Server(t, s3TestAccessKey, s3TestSecretKey, "us-east-1")
	defer server.Close()

	cfg := common.ConfigurationStorage{
		Backend:     "s3",
		S3Endpoint:  server.URL,
		S3Region:    "us-east-1",
		S3Bucket:    "test-bucket",
		S3AccessKey: s3TestAccessKey,
		S3SecretKey: s3TestSecretKey,
		S3PathStyle: true,
	}
	store, err := NewS3BlobStore(cfg)
	if err != nil {
		t.Fatalf("NewS3BlobStore failed: %v", err)
	}
	defer store.Close()

	original := []byte("authentic content!")
	tampered := bytes.Repeat([]byte("x"), len(original))

	// Sign a request over the authentic body, then swap the body before send.
	req, err := store.newRequest("PUT", "hash123", bytes.NewReader(original), hexSHA256(original))
	if err != nil {
		t.Fatalf("newRequest failed: %v", err)
	}
	req.ContentLength = int64(len(original))
	req.Body = io.NopCloser(bytes.NewReader(tampered))

	resp, err := store.client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("tampered body must be rejected by SIGNED_PAYLOAD verification, got status %d", resp.StatusCode)
	}
}

func TestS3BlobStore_UNSIGNEDPayloadStillTolerated(t *testing.T) {
	defer goleak.VerifyNone(t)

	server, _ := verifyingS3Server(t, s3TestAccessKey, s3TestSecretKey, "us-east-1")
	defer server.Close()

	cfg := common.ConfigurationStorage{
		Backend:     "s3",
		S3Endpoint:  server.URL,
		S3Region:    "us-east-1",
		S3Bucket:    "test-bucket",
		S3AccessKey: s3TestAccessKey,
		S3SecretKey: s3TestSecretKey,
		S3PathStyle: true,
	}
	store, err := NewS3BlobStore(cfg)
	if err != nil {
		t.Fatalf("NewS3BlobStore failed: %v", err)
	}
	defer store.Close()

	body := []byte("presigned-style upload")
	req, err := store.newRequest("PUT", "hash456", bytes.NewReader(body), "UNSIGNED-PAYLOAD")
	if err != nil {
		t.Fatalf("newRequest failed: %v", err)
	}
	req.ContentLength = int64(len(body))

	resp, err := store.client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("UNSIGNED-PAYLOAD request must still be accepted (presigned/aws-cli compatibility), got %d", resp.StatusCode)
	}
}
