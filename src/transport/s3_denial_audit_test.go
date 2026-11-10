package transport

import (
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

// denialHook counts refusals the way MetricsCollector does.
type denialHook struct {
	authz int
	quota int
}

func (d *denialHook) IncDownloads()             {}
func (d *denialHook) IncDeletes()               {}
func (d *denialHook) AddBytesDownloaded(uint64) {}
func (d *denialHook) IncReplication()           {}
func (d *denialHook) IncErrors()                {}
func (d *denialHook) IncAuthzDenial()           { d.authz++ }
func (d *denialHook) IncQuotaRefusal()          { d.quota++ }

// auditEntries returns the audit log entries recorded so far.
func auditEntries(t *testing.T, store *storage.CASStore) []*common.AuditLogEntry {
	t.Helper()
	entries, err := store.GetAuditLogEntries(0, time.Now().Add(time.Minute).UnixNano(), 100)
	if err != nil {
		t.Fatalf("GetAuditLogEntries: %v", err)
	}
	return entries
}

func findAudit(entries []*common.AuditLogEntry, operation, outcome string) *common.AuditLogEntry {
	for _, e := range entries {
		if e.Operation == operation && e.Outcome == outcome {
			return e
		}
	}
	return nil
}

// TestS3AuthzDenialIsAuditedAndCounted proves an ACL refusal leaves a trace:
// without it a tenant probing another tenant's bucket is invisible (#1319).
func TestS3AuthzDenialIsAuditedAndCounted(t *testing.T) {
	store := newTenantS3Store(t)
	hook := &denialHook{}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()

	go func() {
		conn, aerr := l.Accept()
		if aerr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		comm := NewS3Communicator(conn)
		comm.SetStore(store)
		comm.SetMetricsHook(hook)
		comm.SetConfiguredBucket("bucket")
		_, _, _ = comm.HandshakeServer([]byte(common.PadString(s3ReqAuth, common.AuthTokenLength))) // notsecret
	}()

	conn, derr := net.Dial("tcp", l.Addr().String())
	if derr != nil {
		t.Fatalf("dial: %v", derr)
	}
	req := strings.Replace(s3Req("GET", "/bucket/docs/a.txt", ""),
		"Authorization: Bearer "+s3ReqAuth, "Authorization: Bearer "+tenantTestToken, 1)
	_, _ = conn.Write([]byte(req))
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()
	}
	buf := make([]byte, 4096)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _ = conn.Read(buf)
	_ = conn.Close()

	if hook.authz != 1 {
		t.Fatalf("expected one authorization denial, got %d", hook.authz)
	}
	entries := auditEntries(t, store)
	entry := findAudit(entries, "Authorize", "denied")
	if entry == nil {
		t.Fatalf("no denied audit entry; entries: %+v", entries)
	}
	if entry.TenantID != "acme" {
		t.Fatalf("denial must be attributed to the tenant, got %q", entry.TenantID)
	}
}

// TestS3QuotaRefusalIsAuditedAndCounted exercises the refusal the ingest path
// reports back through OnStorageError.
func TestS3QuotaRefusalIsAuditedAndCounted(t *testing.T) {
	store := newTenantS3Store(t)
	hook := &denialHook{}
	// Let the tenant through authorization so the refusal under test is the quota.
	if err := store.PutBucketACL(&common.BucketACL{
		Bucket:  "bucket",
		Entries: []common.ACLEntry{{TenantID: "acme", Permission: common.PermWrite, Effect: "allow"}},
	}); err != nil {
		t.Fatalf("PutBucketACL: %v", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, aerr := l.Accept()
		if aerr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		comm := NewS3Communicator(conn)
		comm.SetStore(store)
		comm.SetMetricsHook(hook)
		comm.SetConfiguredBucket("bucket")
		if _, _, herr := comm.HandshakeServer([]byte(common.PadString(s3ReqAuth, common.AuthTokenLength))); herr != nil { // notsecret
			return
		}
		comm.meta.Name = "too-big.bin"
		_ = comm.OnStorageError(storage.ErrQuotaExceeded)
	}()

	conn, derr := net.Dial("tcp", l.Addr().String())
	if derr != nil {
		t.Fatalf("dial: %v", derr)
	}
	req := strings.Replace(s3Req("PUT", "/bucket/too-big.bin", strings.Repeat("x", 16)),
		"Authorization: Bearer "+s3ReqAuth, "Authorization: Bearer "+tenantTestToken, 1)
	_, _ = conn.Write([]byte(req))
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	respBytes, _ := io.ReadAll(conn)
	_ = conn.Close()
	<-done

	if !strings.Contains(string(respBytes), "QuotaExceeded") {
		t.Fatalf("expected a QuotaExceeded response, got:\n%s", string(respBytes))
	}
	if hook.quota != 1 {
		t.Fatalf("expected one quota refusal, got %d", hook.quota)
	}
	entry := findAudit(auditEntries(t, store), "PutObject", "denied")
	if entry == nil {
		t.Fatal("no denied PutObject audit entry")
	}
	if entry.TenantID != "acme" {
		t.Fatalf("quota refusal must be attributed to the tenant, got %q", entry.TenantID)
	}
}

// TestOnStorageErrorIgnoresNonQuotaErrors guards the other direction: a generic
// storage error is not a denial and must not be counted as one.
func TestOnStorageErrorIgnoresNonQuotaErrors(t *testing.T) {
	store := newTenantS3Store(t)
	hook := &denialHook{}

	l, lerr := net.Listen("tcp", "127.0.0.1:0")
	if lerr != nil {
		t.Fatalf("listen: %v", lerr)
	}
	defer func() { _ = l.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, aerr := l.Accept()
		if aerr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		comm := NewS3Communicator(conn)
		comm.SetStore(store)
		comm.SetMetricsHook(hook)
		_ = comm.OnStorageError(fmt.Errorf("disk on fire"))
	}()

	conn, derr := net.Dial("tcp", l.Addr().String())
	if derr != nil {
		t.Fatalf("dial: %v", derr)
	}
	_ = conn.Close()
	<-done

	if hook.quota != 0 || hook.authz != 0 {
		t.Fatalf("a non-quota error must not count as a denial: quota=%d authz=%d", hook.quota, hook.authz)
	}
	if entries := auditEntries(t, store); len(entries) != 0 {
		t.Fatalf("a non-quota error must not write a denied entry: %+v", entries)
	}
}

// TestQuotaRefusalMapsToForbidden pins the wire contract referenced by #1319.
func TestQuotaRefusalMapsToForbidden(t *testing.T) {
	store := newTenantS3Store(t)
	_ = store

	l, lerr := net.Listen("tcp", "127.0.0.1:0")
	if lerr != nil {
		t.Fatalf("listen: %v", lerr)
	}
	defer func() { _ = l.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, aerr := l.Accept()
		if aerr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		comm := NewS3Communicator(conn)
		comm.SetStore(store)
		_ = comm.OnStorageError(storage.ErrQuotaExceeded)
	}()

	conn, derr := net.Dial("tcp", l.Addr().String())
	if derr != nil {
		t.Fatalf("dial: %v", derr)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	respBytes, _ := io.ReadAll(conn)
	_ = conn.Close()
	<-done

	resp := string(respBytes)
	if !strings.Contains(resp, "403 Forbidden") {
		t.Fatalf("expected 403, got:\n%s", resp)
	}
}

// TestRepeatedDenialAuditsOnce proves the wiring behind #1321: the refusal is
// counted every time, but the audit trail is sampled, so a client cannot grow the
// audit bucket at request rate.
func TestRepeatedDenialAuditsOnce(t *testing.T) {
	store := newTenantS3Store(t)
	server, client := net.Pipe()
	defer func() { _ = client.Close() }()

	comm := NewS3Communicator(server)
	comm.SetStore(store)
	comm.tenantID = "acme"

	for i := 0; i < 20; i++ {
		comm.writeAuditLog("Authorize", "object/bucket/docs/a.txt", "denied")
	}
	_ = server.Close()

	var denials int
	for _, e := range auditEntries(t, store) {
		if e.Operation == "Authorize" && e.Outcome == "denied" {
			denials++
		}
	}
	if denials != 1 {
		t.Fatalf("20 identical denials must produce 1 audit entry, got %d", denials)
	}
}

// TestSuccessfulAuditIsNeverSuppressed guards the obvious over-reach: the limiter
// applies to denials only.
func TestSuccessfulAuditIsNeverSuppressed(t *testing.T) {
	store := newTenantS3Store(t)
	server, client := net.Pipe()
	defer func() { _ = client.Close() }()

	comm := NewS3Communicator(server)
	comm.SetStore(store)
	comm.tenantID = "acme"

	for i := 0; i < 20; i++ {
		comm.writeAuditLog("GetObject", "object/bucket/docs/a.txt", "success")
	}
	_ = server.Close()

	var successes int
	for _, e := range auditEntries(t, store) {
		if e.Operation == "GetObject" && e.Outcome == "success" {
			successes++
		}
	}
	if successes != 20 {
		t.Fatalf("successful operations must always be audited, got %d of 20", successes)
	}
}
