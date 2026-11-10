package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"syscall"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
)

// tenantComm adds the tenant identity the S3 gateway exposes to the ingest path,
// so the shared writer can charge quotas without knowing about S3.
type tenantComm struct {
	transport.Communicator
	tenant string
}

func (c tenantComm) TenantID() string { return c.tenant }

// newQuotaStore builds a store with one tenant account configured.
func newQuotaStore(t *testing.T, tenant string, quotaBytes int64) *storage.CASStore {
	t.Helper()
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.PutTenantConfig(&common.TenantConfig{
		ID:          tenant,
		MasterKeyID: "root-1",
		AuthToken:   "token",
		QuotaBytes:  quotaBytes,
		Enabled:     true,
	}); err != nil {
		t.Fatalf("PutTenantConfig: %v", err)
	}
	return store
}

// streamComm returns a communicator that streams payload, optionally tenant-tagged.
func quotaStreamComm(t *testing.T, payload, tenant string) transport.Communicator {
	t.Helper()
	server, client := net.Pipe()
	go func() {
		defer func() { _ = client.Close() }()
		_, _ = client.Write([]byte(payload))
	}()
	t.Cleanup(func() { _ = server.Close() })
	base := transport.NewMomoTCPCommunicator(server)
	if tenant == "" {
		return base
	}
	return tenantComm{Communicator: base, tenant: tenant}
}

func payloadHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TestGetFile_QuotaRefusedBeforeWrite proves the refusal happens before the
// payload is persisted, and that no usage is recorded for it.
func TestGetFile_QuotaRefusedBeforeWrite(t *testing.T) {
	payload := "0123456789ABCDEF" // 16 bytes against a 10-byte quota
	store := newQuotaStore(t, "acme", 10)
	comm := quotaStreamComm(t, payload, "acme")

	err := getFile(comm, store, "too-big.bin", payloadHash(payload), int64(len(payload)), "")
	if !errors.Is(err, syscall.EDQUOT) {
		t.Fatalf("expected a quota refusal, got %v", err)
	}
	if _, gerr := store.GetMeta("too-big.bin"); gerr == nil {
		t.Fatal("an over-quota object must not be stored")
	}
	if b, o, _ := store.TenantUsage("acme"); b != 0 || o != 0 {
		t.Fatalf("refused write must leave usage untouched: bytes=%d objects=%d", b, o)
	}
}

func TestGetFile_QuotaChargedOnSuccess(t *testing.T) {
	payload := "12345"
	store := newQuotaStore(t, "acme", 100)

	if err := getFile(quotaStreamComm(t, payload, "acme"), store, "ok.bin", payloadHash(payload), int64(len(payload)), ""); err != nil {
		t.Fatalf("write within quota: %v", err)
	}
	if b, o, _ := store.TenantUsage("acme"); b != 5 || o != 1 {
		t.Fatalf("usage after write: bytes=%d objects=%d", b, o)
	}
}

// TestGetFile_QuotaOverwritePaysOnlyTheDelta pins the overwrite accounting: the
// object count stays at 1 and the bytes move to the new size.
func TestGetFile_QuotaOverwritePaysOnlyTheDelta(t *testing.T) {
	first, second := "12345", "12345678"
	store := newQuotaStore(t, "acme", 100)

	if err := getFile(quotaStreamComm(t, first, "acme"), store, "same.bin", payloadHash(first), int64(len(first)), ""); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := getFile(quotaStreamComm(t, second, "acme"), store, "same.bin", payloadHash(second), int64(len(second)), ""); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if b, o, _ := store.TenantUsage("acme"); b != 8 || o != 1 {
		t.Fatalf("overwrite must charge the delta only: bytes=%d objects=%d", b, o)
	}
}

// TestGetFile_QuotaReleaseOnHashMismatch proves a rejected payload does not keep
// its reservation.
func TestGetFile_QuotaReleaseOnHashMismatch(t *testing.T) {
	payload := "abcdef"
	store := newQuotaStore(t, "acme", 100)

	if err := getFile(quotaStreamComm(t, payload, "acme"), store, "bad.bin", payloadHash("different"), int64(len(payload)), ""); err == nil {
		t.Fatal("hash mismatch must fail the write")
	}
	if b, o, _ := store.TenantUsage("acme"); b != 0 || o != 0 {
		t.Fatalf("failed write must release its reservation: bytes=%d objects=%d", b, o)
	}
	if _, gerr := store.GetMeta("bad.bin"); gerr == nil {
		t.Fatal("a hash-mismatched object must be deleted")
	}
}

// TestGetFile_NoTenantLeavesQuotasAlone covers the native/peer surface, which
// carries no tenant identity.
func TestGetFile_NoTenantLeavesQuotasAlone(t *testing.T) {
	payload := "native"
	store := newQuotaStore(t, "acme", 1)

	if err := getFile(quotaStreamComm(t, payload, ""), store, "native.bin", payloadHash(payload), int64(len(payload)), ""); err != nil {
		t.Fatalf("tenant-less write must not be quota-checked: %v", err)
	}
	if b, o, _ := store.TenantUsage("acme"); b != 0 || o != 0 {
		t.Fatalf("tenant-less write must not be charged: bytes=%d objects=%d", b, o)
	}
}
