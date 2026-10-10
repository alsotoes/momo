package crypto

import (
	"bytes"
	"testing"
)

func testRoot() []byte {
	k := make([]byte, KeySize)
	for i := range k {
		k[i] = byte(i + 1)
	}
	return k
}

func TestDeriveTenantKeys_InvalidKEKSize(t *testing.T) {
	if _, err := DeriveTenantKeys([]byte("too-short"), "t1"); err != ErrInvalidKeySize {
		t.Fatalf("expected ErrInvalidKeySize, got %v", err)
	}
}

func TestDeriveTenantKeys_SizesAndIsolation(t *testing.T) {
	root := testRoot()
	a, err := DeriveTenantKeys(root, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.KEK) != TenantKEKSize || len(a.OPRFShare) != TenantOPRFShareSize || len(a.AuthToken) != TenantAuthTokenSize {
		t.Fatalf("bad sizes: %d %d %d", len(a.KEK), len(a.OPRFShare), len(a.AuthToken))
	}
	b, _ := DeriveTenantKeys(root, "tenant-b")
	if bytes.Equal(a.KEK, b.KEK) || bytes.Equal(a.AuthToken, b.AuthToken) || bytes.Equal(a.OPRFShare, b.OPRFShare) {
		t.Fatal("tenants must not share derived key material")
	}
}

func TestWrapUnwrapTenantKEK_RoundTrip(t *testing.T) {
	root := testRoot()
	keys, _ := DeriveTenantKeys(root, "tenant-x")
	wrapped, err := WrapTenantKEK(root, keys.KEK, "tenant-x")
	if err != nil {
		t.Fatal(err)
	}
	unwrapped, err := UnwrapTenantKEK(root, wrapped, "tenant-x")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unwrapped, keys.KEK) {
		t.Fatal("round-trip mismatch")
	}
}

func TestUnwrapTenantKEK_Errors(t *testing.T) {
	root := testRoot()
	if _, err := UnwrapTenantKEK([]byte("short"), nil, "t"); err != ErrInvalidKeySize {
		t.Fatalf("expected ErrInvalidKeySize, got %v", err)
	}
	wrapped, err := WrapTenantKEK(root, make([]byte, TenantKEKSize), "t")
	if err != nil {
		t.Fatal(err)
	}
	bad := append([]byte{}, wrapped...)
	bad[0] ^= 0xff
	if _, err := UnwrapTenantKEK(root, bad, "t"); err != ErrTampered {
		t.Fatalf("expected ErrTampered, got %v", err)
	}
	// Wrong tenant AAD must fail.
	if _, err := UnwrapTenantKEK(root, wrapped, "other"); err == nil {
		t.Fatal("expected AAD mismatch error")
	}
}

func TestWrapTenantKEK_InvalidSizes(t *testing.T) {
	if _, err := WrapTenantKEK([]byte("short"), make([]byte, TenantKEKSize), "t"); err != ErrInvalidKeySize {
		t.Fatalf("expected ErrInvalidKeySize for root, got %v", err)
	}
	if _, err := WrapTenantKEK(testRoot(), []byte("short"), "t"); err != ErrInvalidKeySize {
		t.Fatalf("expected ErrInvalidKeySize for tenant KEK, got %v", err)
	}
}

func TestDeriveTenantOPRFShare_Extra(t *testing.T) {
	if _, err := DeriveTenantOPRFShare([]byte("short"), "t"); err != ErrInvalidKeySize {
		t.Fatalf("expected ErrInvalidKeySize, got %v", err)
	}
	root := make([]byte, TenantOPRFShareSize)
	got, err := DeriveTenantOPRFShare(root, "t")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != TenantOPRFShareSize {
		t.Fatalf("bad size %d", len(got))
	}
	other, _ := DeriveTenantOPRFShare(root, "u")
	if bytes.Equal(got, other) {
		t.Fatal("different tenants must derive different OPRF shares")
	}
}
