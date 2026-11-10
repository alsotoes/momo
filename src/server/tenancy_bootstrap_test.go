package server

import (
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
)

// fakeTenancyStore is an in-memory tenancySeeder, so the bootstrap semantics are
// tested without a CAS store.
type fakeTenancyStore struct {
	tenants map[string]*common.TenantConfig
	acls    map[string]*common.BucketACL
	puts    int
	failPut error
}

func newFakeTenancyStore() *fakeTenancyStore {
	return &fakeTenancyStore{
		tenants: map[string]*common.TenantConfig{},
		acls:    map[string]*common.BucketACL{},
	}
}

func (f *fakeTenancyStore) PutTenantConfig(t *common.TenantConfig) error {
	if f.failPut != nil {
		return f.failPut
	}
	f.puts++
	cp := *t
	f.tenants[t.ID] = &cp
	return nil
}

func (f *fakeTenancyStore) ListTenantConfigs() ([]*common.TenantConfig, error) {
	out := make([]*common.TenantConfig, 0, len(f.tenants))
	for _, t := range f.tenants {
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeTenancyStore) DeleteTenantConfig(id string) error {
	delete(f.tenants, id)
	return nil
}

func (f *fakeTenancyStore) PutBucketACL(a *common.BucketACL) error {
	if f.failPut != nil {
		return f.failPut
	}
	f.puts++
	f.acls[a.Bucket] = a
	return nil
}

func (f *fakeTenancyStore) ListBucketACLs() ([]*common.BucketACL, error) {
	out := make([]*common.BucketACL, 0, len(f.acls))
	for _, a := range f.acls {
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeTenancyStore) DeleteBucketACL(bucket string) error {
	delete(f.acls, bucket)
	return nil
}

func tenantCfg(id string) *common.TenantConfig {
	return &common.TenantConfig{ID: id, MasterKeyID: "root-1", AuthToken: "token-" + id, Enabled: true}
}

// aclCfg builds one allow entry for the tenant on the bucket.
func aclCfg(bucket, tenant string) *common.BucketACL {
	return &common.BucketACL{
		Bucket:  bucket,
		Entries: []common.ACLEntry{{TenantID: tenant, Permission: common.PermRead, Effect: "allow"}},
	}
}

// TestBootstrapTenancySeedsDeclaredEntries is the fix for #1325: config becomes
// the source of truth, so a declared tenant can authenticate and a declared ACL
// grants access.
func TestBootstrapTenancySeedsDeclaredEntries(t *testing.T) {
	store := newFakeTenancyStore()
	cfg := common.Configuration{
		Tenants: map[string]*common.TenantConfig{"acme": tenantCfg("acme"), "beta": tenantCfg("beta")},
		ACLs:    map[string]*common.BucketACL{"photos": aclCfg("photos", "acme")},
	}

	if err := bootstrapTenancy(store, cfg); err != nil {
		t.Fatalf("bootstrapTenancy: %v", err)
	}
	if len(store.tenants) != 2 || store.tenants["acme"].AuthToken != "token-acme" {
		t.Fatalf("tenants not seeded: %+v", store.tenants)
	}
	if acl := store.acls["photos"]; acl == nil || len(acl.Entries) != 1 {
		t.Fatalf("bucket ACL not seeded: %+v", store.acls)
	}
}

// TestBootstrapTenancyIsIdempotent: restarting must not duplicate or fail.
func TestBootstrapTenancyIsIdempotent(t *testing.T) {
	store := newFakeTenancyStore()
	cfg := common.Configuration{
		Tenants: map[string]*common.TenantConfig{"acme": tenantCfg("acme")},
		ACLs:    map[string]*common.BucketACL{"photos": aclCfg("photos", "acme")},
	}
	for i := 0; i < 3; i++ {
		if err := bootstrapTenancy(store, cfg); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if len(store.tenants) != 1 || len(store.acls) != 1 {
		t.Fatalf("idempotent bootstrap drifted: tenants=%d acls=%d", len(store.tenants), len(store.acls))
	}
}

// TestBootstrapTenancyPrunesRemovedEntries proves removing a tenant revokes it:
// leaving it in the store would keep a removed credential valid.
func TestBootstrapTenancyPrunesRemovedEntries(t *testing.T) {
	store := newFakeTenancyStore()
	if err := bootstrapTenancy(store, common.Configuration{
		Tenants: map[string]*common.TenantConfig{"acme": tenantCfg("acme"), "beta": tenantCfg("beta")},
		ACLs:    map[string]*common.BucketACL{"photos": aclCfg("photos", "acme"), "docs": aclCfg("docs", "beta")},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := bootstrapTenancy(store, common.Configuration{
		Tenants: map[string]*common.TenantConfig{"acme": tenantCfg("acme")},
		ACLs:    map[string]*common.BucketACL{"photos": aclCfg("photos", "acme")},
	}); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, ok := store.tenants["beta"]; ok {
		t.Fatal("removed tenant beta must be pruned")
	}
	if _, ok := store.acls["docs"]; ok {
		t.Fatal("removed bucket ACL must be pruned")
	}
	if len(store.tenants) != 1 || len(store.acls) != 1 {
		t.Fatalf("pruned too much: tenants=%d acls=%d", len(store.tenants), len(store.acls))
	}
}

// TestBootstrapTenancyEmptySectionsNeverWipe is the safety property: an absent
// section (the common case, tenancy unused) must not delete stored state.
func TestBootstrapTenancyEmptySectionsNeverWipe(t *testing.T) {
	store := newFakeTenancyStore()
	if err := bootstrapTenancy(store, common.Configuration{
		Tenants: map[string]*common.TenantConfig{"acme": tenantCfg("acme")},
		ACLs:    map[string]*common.BucketACL{"photos": aclCfg("photos", "acme")},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := bootstrapTenancy(store, common.Configuration{}); err != nil {
		t.Fatalf("empty config: %v", err)
	}
	if len(store.tenants) != 1 || len(store.acls) != 1 {
		t.Fatalf("an empty config must not wipe state: tenants=%d acls=%d", len(store.tenants), len(store.acls))
	}
}

// TestBootstrapTenancyPropagatesStoreErrors keeps a broken store visible at
// startup instead of running with half-applied tenancy.
func TestBootstrapTenancyPropagatesStoreErrors(t *testing.T) {
	store := newFakeTenancyStore()
	store.failPut = syscall.EIO
	err := bootstrapTenancy(store, common.Configuration{
		Tenants: map[string]*common.TenantConfig{"acme": tenantCfg("acme")},
	})
	if err == nil {
		t.Fatal("a store error must fail the bootstrap")
	}
	if !errors.Is(err, syscall.EIO) {
		t.Fatalf("error must wrap the store error, got %v", err)
	}
}

// TestBootstrapTenancyNilStoreIsANoOp keeps callers that have no tenancy (or a
// non-CAS store) working.
func TestBootstrapTenancyNilStoreIsANoOp(t *testing.T) {
	if err := bootstrapTenancy(nil, common.Configuration{
		Tenants: map[string]*common.TenantConfig{"acme": tenantCfg("acme")},
	}); err != nil {
		t.Fatalf("nil store must be a no-op, got %v", err)
	}
}

// TestConfiguredTenantIsUsableEndToEnd is the proof that #1325 fixes the gap: a
// tenant declared only in configuration now authenticates and is allowed by a
// declared bucket ACL, over a real S3 connection and a real CAS store. Before the
// bootstrap the same setup answered 403 at the handshake, because tenant_meta was
// empty.
func TestConfiguredTenantIsUsableEndToEnd(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	const tenantToken = "acme-token-0123456789abcdef0123456789abcdef" // notsecret
	cfg := common.Configuration{
		Tenants: map[string]*common.TenantConfig{
			"acme": {ID: "acme", MasterKeyID: "root-1", AuthToken: tenantToken, Enabled: true},
		},
		ACLs: map[string]*common.BucketACL{
			"photos": {
				Bucket:  "photos",
				Entries: []common.ACLEntry{{TenantID: "acme", Permission: common.PermRead, Effect: "allow"}},
			},
		},
	}
	if err := bootstrapTenancy(store, cfg); err != nil {
		t.Fatalf("bootstrapTenancy: %v", err)
	}

	status, _ := s3GetAsTenant(t, store, tenantToken, "/photos/missing.txt")
	if strings.Contains(status, "403") {
		t.Fatalf("declared ACL must allow the read, got:\n%s", status)
	}
	// The bucket has no object, so reaching dispatch means the ACL permitted it.
	if !strings.Contains(status, "404") {
		t.Fatalf("expected dispatch to answer 404, got:\n%s", status)
	}

	// Default-deny is preserved: an undeclared bucket is still refused.
	deniedStatus, _ := s3GetAsTenant(t, store, tenantToken, "/docs/missing.txt")
	if !strings.Contains(deniedStatus, "403") {
		t.Fatalf("a bucket without an ACL must be denied, got:\n%s", deniedStatus)
	}
}

// s3GetAsTenant runs one GET through a real S3 communicator over net.Pipe and
// returns whether the tenant authenticated plus the raw response head.
func s3GetAsTenant(t *testing.T, store *storage.CASStore, token, target string) (string, error) {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })

	comm := transport.NewS3Communicator(server)
	comm.SetStore(store)
	comm.SetConfiguredBucket("photos")

	done := make(chan struct{})
	var handshakeErr error
	go func() {
		defer close(done)
		defer func() { _ = server.Close() }()
		_, _, handshakeErr = comm.HandshakeServer([]byte(common.PadString("cluster-token-0123456789abcdef0123456789abcdef", common.AuthTokenLength))) // notsecret
	}()

	if _, err := client.Write([]byte("GET " + target + " HTTP/1.1\r\nHost: 127.0.0.1:4440\r\nAuthorization: Bearer " + token + "\r\n\r\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 4096)
	n, _ := client.Read(buf)
	<-done
	return string(buf[:n]), handshakeErr
}
