package transport

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

const tenantTestToken = "tenant-acme-token-0000000000000000000000000000000000000000000000" // notsecret

// runS3AsTenant sends one raw S3 request authenticated with bearerToken and
// returns the raw response.
func runS3AsTenant(t *testing.T, reqStr, bearerToken string, store storage.Store, bucket string) (string, error) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()

	expected := []byte(common.PadString(s3ReqAuth, common.AuthTokenLength)) // notsecret
	errChan := make(chan error, 1)
	go func() {
		conn, aerr := l.Accept()
		if aerr != nil {
			errChan <- aerr
			return
		}
		defer func() { _ = conn.Close() }()
		comm := NewS3Communicator(conn)
		comm.SetStore(store)
		comm.SetConfiguredBucket(bucket)
		_, _, herr := comm.HandshakeServer(expected)
		errChan <- herr
	}()

	conn, derr := net.Dial("tcp", l.Addr().String())
	if derr != nil {
		t.Fatalf("dial: %v", derr)
	}
	defer func() { _ = conn.Close() }()
	req := strings.Replace(reqStr, "Authorization: Bearer "+s3ReqAuth,
		"Authorization: Bearer "+bearerToken, 1)
	if _, werr := conn.Write([]byte(req)); werr != nil {
		t.Fatalf("write request: %v", werr)
	}
	// Half-close so the server sees EOF without waiting for the deadline.
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()
	}
	respBytes, _ := io.ReadAll(conn)
	return string(respBytes), <-errChan
}

func newTenantS3Store(t *testing.T) *storage.CASStore {
	t.Helper()
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.PutTenantConfig(&common.TenantConfig{
		ID:          "acme",
		MasterKeyID: "root-1",
		AuthToken:   tenantTestToken,
		Enabled:     true,
	}); err != nil {
		t.Fatalf("PutTenantConfig: %v", err)
	}
	return store
}

// TestS3TenantTokenAuthenticatesAndIsDefaultDenied proves the tenant credential
// is accepted (#1316) and that ACL evaluation now actually runs: without an ACL
// the request is refused rather than silently served.
func TestS3TenantTokenAuthenticatesAndIsDefaultDenied(t *testing.T) {
	store := newTenantS3Store(t)

	resp, err := runS3AsTenant(t, s3Req("GET", "/bucket/docs/a.txt", ""), tenantTestToken, store, "bucket")
	if err == nil {
		t.Fatalf("a tenant request without an ACL must be refused, got response:\n%s", resp)
	}
	if !strings.Contains(resp, "403 Forbidden") || !strings.Contains(resp, "AccessDenied") {
		t.Fatalf("expected 403 AccessDenied, got:\n%s", resp)
	}
}

// TestS3TenantTokenAllowedByACL proves an allow grant lets the tenant through to
// normal dispatch (404 here, because the object does not exist).
func TestS3TenantTokenAllowedByACL(t *testing.T) {
	store := newTenantS3Store(t)
	if err := store.PutBucketACL(&common.BucketACL{
		Bucket: "bucket",
		Entries: []common.ACLEntry{
			{TenantID: "acme", Permission: common.PermRead, Effect: "allow"},
		},
	}); err != nil {
		t.Fatalf("PutBucketACL: %v", err)
	}

	resp, _ := runS3AsTenant(t, s3Req("GET", "/bucket/docs/a.txt", ""), tenantTestToken, store, "bucket")
	if !strings.Contains(resp, "404") {
		t.Fatalf("an allowed tenant request must reach dispatch (404 NoSuchKey), got:\n%s", resp)
	}
	if strings.Contains(resp, "AccessDenied") {
		t.Fatalf("allowed tenant must not be denied:\n%s", resp)
	}
}

// TestS3UnknownTokenStillDenied guards the other direction: resolution must not
// turn the handshake into an open door.
func TestS3UnknownTokenStillDenied(t *testing.T) {
	store := newTenantS3Store(t)
	resp, err := runS3AsTenant(t, s3Req("GET", "/bucket/docs/a.txt", ""), "not-a-configured-token", store, "bucket")
	if err == nil {
		t.Fatalf("unknown credential must be rejected, got:\n%s", resp)
	}
	if !strings.Contains(resp, "403 Forbidden") {
		t.Fatalf("expected 403, got:\n%s", resp)
	}
}

// TestS3ClusterTokenHasNoTenant proves the admin surface keeps working and is not
// attributed to any tenant.
func TestS3ClusterTokenHasNoTenant(t *testing.T) {
	store := newTenantS3Store(t)
	resp, _ := runS3AsTenant(t, s3Req("GET", "/bucket/docs/a.txt", ""), s3ReqAuth, store, "bucket")
	if strings.Contains(resp, "AccessDenied") {
		t.Fatalf("the cluster token must not hit tenant ACLs:\n%s", resp)
	}
}

// TestS3TenantTokenResolvesAtHandshake pins the binding itself, which is what
// makes ACLs, quotas and tenant-attributed audit entries reachable.
func TestS3TenantTokenResolvesAtHandshake(t *testing.T) {
	store := newTenantS3Store(t)
	if tid, err := store.GetTenantIDByAuthToken(tenantTestToken); err != nil || tid != "acme" {
		t.Fatalf("store lookup: tid=%q err=%v", tid, err)
	}

	l, lerr := net.Listen("tcp", "127.0.0.1:0")
	if lerr != nil {
		t.Fatalf("listen: %v", lerr)
	}
	defer func() { _ = l.Close() }()

	got := make(chan string, 1)
	go func() {
		conn, aerr := l.Accept()
		if aerr != nil {
			got <- "accept-failed"
			return
		}
		defer func() { _ = conn.Close() }()
		comm := NewS3Communicator(conn)
		comm.SetStore(store)
		comm.SetConfiguredBucket("bucket")
		_, _, _ = comm.HandshakeServer([]byte(common.PadString(s3ReqAuth, common.AuthTokenLength))) // notsecret
		got <- comm.TenantID()
	}()

	conn, derr := net.Dial("tcp", l.Addr().String())
	if derr != nil {
		t.Fatalf("dial: %v", derr)
	}
	defer func() { _ = conn.Close() }()
	req := strings.Replace(s3Req("GET", "/bucket/docs/a.txt", ""),
		"Authorization: Bearer "+s3ReqAuth, "Authorization: Bearer "+tenantTestToken, 1)
	_, _ = conn.Write([]byte(req))
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()
	}
	_, _ = io.ReadAll(conn)

	select {
	case tid := <-got:
		if tid != "acme" {
			t.Fatalf("handshake must bind the tenant, got %q", tid)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the handshake")
	}
}

// TestS3OversizedCredentialIsRefusedNotPanicked pins the pre-comparison guard:
// a credential longer than the token width used to reach PadString, which panics
// (recovered, but the client got no response). It must be a clean 403.
func TestS3OversizedCredentialIsRefusedNotPanicked(t *testing.T) {
	store := newTenantS3Store(t)
	huge := strings.Repeat("x", 4096)
	resp, err := runS3AsTenant(t, s3Req("GET", "/bucket/docs/a.txt", ""), huge, store, "bucket")
	if err == nil {
		t.Fatalf("an oversized credential must be refused, got:\n%s", resp)
	}
	if !strings.Contains(resp, "403 Forbidden") {
		t.Fatalf("expected a clean 403, got:\n%s", resp)
	}
}
