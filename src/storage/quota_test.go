package storage

import (
	"errors"
	"sync"
	"syscall"
	"testing"

	"github.com/alsotoes/momo/src/common"
)

func newQuotaStore(t *testing.T) *CASStore {
	t.Helper()
	s, err := NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedTenant(t *testing.T, s *CASStore, id string, quotaBytes, quotaObjects int64) {
	t.Helper()
	if err := s.PutTenantConfig(&common.TenantConfig{
		ID:           id,
		MasterKeyID:  "root-1",
		AuthToken:    "token-" + id,
		QuotaBytes:   quotaBytes,
		QuotaObjects: quotaObjects,
		Enabled:      true,
	}); err != nil {
		t.Fatalf("PutTenantConfig: %v", err)
	}
}

func TestTenantQuota_NoOpCases(t *testing.T) {
	s := newQuotaStore(t)
	seedTenant(t, s, "unlimited", 0, 0)

	for _, tc := range []struct {
		name      string
		tenant    string
		deltaByte int64
	}{
		{"empty tenant", "", 1000},
		{"unknown tenant", "ghost", 1000},
		{"unlimited quotas", "unlimited", 1 << 40},
	} {
		if err := s.AdjustTenantQuota(tc.tenant, tc.deltaByte, 1); err != nil {
			t.Fatalf("%s: expected no-op, got %v", tc.name, err)
		}
		if b, o, err := s.TenantUsage(tc.tenant); err != nil || b != 0 || o != 0 {
			t.Fatalf("%s: unlimited/unknown must not be accounted: bytes=%d objects=%d err=%v", tc.name, b, o, err)
		}
	}
}

func TestTenantQuota_ByteLimit(t *testing.T) {
	s := newQuotaStore(t)
	seedTenant(t, s, "acme", 100, 0)

	if err := s.AdjustTenantQuota("acme", 60, 1); err != nil {
		t.Fatalf("first reservation must fit: %v", err)
	}
	if b, o, _ := s.TenantUsage("acme"); b != 60 || o != 1 {
		t.Fatalf("usage after first: bytes=%d objects=%d", b, o)
	}

	err := s.AdjustTenantQuota("acme", 50, 1)
	if !errors.Is(err, syscall.EDQUOT) {
		t.Fatalf("expected EDQUOT, got %v", err)
	}
	if b, o, _ := s.TenantUsage("acme"); b != 60 || o != 1 {
		t.Fatalf("a refused reservation must not be applied: bytes=%d objects=%d", b, o)
	}

	// Exactly at the limit is allowed.
	if err := s.AdjustTenantQuota("acme", 40, 0); err != nil {
		t.Fatalf("hitting the limit exactly must be allowed: %v", err)
	}
}

func TestTenantQuota_ObjectLimit(t *testing.T) {
	s := newQuotaStore(t)
	seedTenant(t, s, "acme", 0, 2)

	if err := s.AdjustTenantQuota("acme", 10, 1); err != nil {
		t.Fatalf("object 1: %v", err)
	}
	if err := s.AdjustTenantQuota("acme", 10, 1); err != nil {
		t.Fatalf("object 2: %v", err)
	}
	if err := s.AdjustTenantQuota("acme", 0, 1); !errors.Is(err, syscall.EDQUOT) {
		t.Fatalf("third object must be refused, got %v", err)
	}
	// Overwrites (no object delta) stay free.
	if err := s.AdjustTenantQuota("acme", 5, 0); err != nil {
		t.Fatalf("overwrite must not consume the object quota: %v", err)
	}
}

func TestTenantQuota_ReleaseClampsAtZero(t *testing.T) {
	s := newQuotaStore(t)
	seedTenant(t, s, "acme", 100, 10)

	if err := s.AdjustTenantQuota("acme", 40, 1); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := s.AdjustTenantQuota("acme", -1000, -5); err != nil {
		t.Fatalf("release: %v", err)
	}
	if b, o, _ := s.TenantUsage("acme"); b != 0 || o != 0 {
		t.Fatalf("release must clamp at zero: bytes=%d objects=%d", b, o)
	}
}

// TestTenantQuota_ConcurrentBoundaryNeverOvershoots is the race that matters: a
// burst of writes at the quota edge must not admit more than the limit allows.
func TestTenantQuota_ConcurrentBoundaryNeverOvershoots(t *testing.T) {
	s := newQuotaStore(t)
	seedTenant(t, s, "acme", 100, 0)

	const writers = 24
	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted := 0
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			if err := s.AdjustTenantQuota("acme", 10, 1); err == nil {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if admitted != 10 {
		t.Fatalf("expected exactly 10 admissions at a 100-byte quota, got %d", admitted)
	}
	if b, o, _ := s.TenantUsage("acme"); b != 100 || o != 10 {
		t.Fatalf("usage must equal the admitted set: bytes=%d objects=%d", b, o)
	}
}
