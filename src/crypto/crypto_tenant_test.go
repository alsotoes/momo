package crypto

import (
	"testing"
)

func TestDeriveTenantKeys_Deterministic(t *testing.T) {
	rootKEK, _ := GenerateKey()
	tenantID := "tenant-a"

	keys1, err := DeriveTenantKeys(rootKEK, tenantID)
	if err != nil {
		t.Fatalf("DeriveTenantKeys: %v", err)
	}

	keys2, err := DeriveTenantKeys(rootKEK, tenantID)
	if err != nil {
		t.Fatalf("DeriveTenantKeys (2nd call): %v", err)
	}

	if !equal(keys1.KEK, keys2.KEK) || !equal(keys1.OPRFShare, keys2.OPRFShare) || !equal(keys1.AuthToken, keys2.AuthToken) {
		t.Fatal("keys differ on second derivation")
	}
}

func TestDeriveTenantKeys_Isolation(t *testing.T) {
	rootKEK, _ := GenerateKey()

	keysA, _ := DeriveTenantKeys(rootKEK, "tenant-a")
	keysB, _ := DeriveTenantKeys(rootKEK, "tenant-b")

	if equal(keysA.KEK, keysB.KEK) {
		t.Fatal("different tenants produced same KEK")
	}
	if equal(keysA.OPRFShare, keysB.OPRFShare) {
		t.Fatal("different tenants produced same OPRF share")
	}
	if equal(keysA.AuthToken, keysB.AuthToken) {
		t.Fatal("different tenants produced same auth token")
	}
}

func TestDeriveTenantKeys_DifferentRoot(t *testing.T) {
	rootKEK1, _ := GenerateKey()
	rootKEK2, _ := GenerateKey()

	keys1, _ := DeriveTenantKeys(rootKEK1, "tenant-a")
	keys2, _ := DeriveTenantKeys(rootKEK2, "tenant-a")

	if equal(keys1.KEK, keys2.KEK) {
		t.Fatal("different roots produced same KEK for same tenant")
	}
}

func TestWrapUnwrapTenantKEK(t *testing.T) {
	rootKEK, _ := GenerateKey()
	tenantKEK, _ := GenerateKey()
	tenantID := "tenant-a"

	wrapped, err := WrapTenantKEK(rootKEK, tenantKEK, tenantID)
	if err != nil {
		t.Fatalf("WrapTenantKEK: %v", err)
	}

	if len(wrapped) != TenantKEKSize+TagSize { // nonce(12) + key(32) + tag(16)
		t.Fatalf("wrapped length %d, expected %d", len(wrapped), TenantKEKSize+12+16)
	}

	unwrapped, err := UnwrapTenantKEK(rootKEK, wrapped, tenantID)
	if err != nil {
		t.Fatalf("UnwrapTenantKEK: %v", err)
	}

	if !equal(tenantKEK, unwrapped) {
		t.Fatal("unwrapped KEK differs from original")
	}
}

func TestWrapTenantKEK_BindsToTenant(t *testing.T) {
	rootKEK, _ := GenerateKey()
	tenantKEK, _ := GenerateKey()

	wrappedA, _ := WrapTenantKEK(rootKEK, tenantKEK, "tenant-a")
	wrappedB, _ := WrapTenantKEK(rootKEK, tenantKEK, "tenant-b")

	if equal(wrappedA, wrappedB) {
		t.Fatal("wrapped KEK should differ per tenant (AAD binding)")
	}
}

func TestUnwrapTenantKEK_WrongTenant(t *testing.T) {
	rootKEK, _ := GenerateKey()
	tenantKEK, _ := GenerateKey()

	wrapped, _ := WrapTenantKEK(rootKEK, tenantKEK, "tenant-a")

	_, err := UnwrapTenantKEK(rootKEK, wrapped, "tenant-b")
	if err == nil {
		t.Fatal("expected error when unwrapping with wrong tenant ID")
	}
}

func TestRotateTenantKEK(t *testing.T) {
	oldRoot, _ := GenerateKey()
	newRoot, _ := GenerateKey()
	tenantKEK, _ := GenerateKey()
	tenantID := "tenant-a"

	wrapped, _ := WrapTenantKEK(oldRoot, tenantKEK, tenantID)

	newWrapped, err := RotateTenantKEK(oldRoot, newRoot, wrapped, tenantID)
	if err != nil {
		t.Fatalf("RotateTenantKEK: %v", err)
	}

	unwrapped, err := UnwrapTenantKEK(newRoot, newWrapped, tenantID)
	if err != nil {
		t.Fatalf("Unwrap after rotation: %v", err)
	}

	if !equal(tenantKEK, unwrapped) {
		t.Fatal("rotated KEK differs from original")
	}
}

func TestRotateTenantKEK_WrongOldRoot(t *testing.T) {
	oldRoot, _ := GenerateKey()
	wrongOldRoot, _ := GenerateKey()
	newRoot, _ := GenerateKey()
	tenantKEK, _ := GenerateKey()
	tenantID := "tenant-a"

	wrapped, _ := WrapTenantKEK(oldRoot, tenantKEK, tenantID)

	_, err := RotateTenantKEK(wrongOldRoot, newRoot, wrapped, tenantID)
	if err == nil {
		t.Fatal("expected error when unwrapping with wrong old root")
	}
}

func TestDeriveTenantOPRFShare(t *testing.T) {
	rootShare, _ := GenerateKey()
	tenantID := "tenant-a"

	share1, err := DeriveTenantOPRFShare(rootShare, tenantID)
	if err != nil {
		t.Fatalf("DeriveTenantOPRFShare: %v", err)
	}

	if len(share1) != TenantOPRFShareSize {
		t.Fatalf("share length %d, expected %d", len(share1), TenantOPRFShareSize)
	}

	share2, _ := DeriveTenantOPRFShare(rootShare, tenantID)
	if !equal(share1, share2) {
		t.Fatal("non-deterministic derivation")
	}

	shareB, _ := DeriveTenantOPRFShare(rootShare, "tenant-b")
	if equal(share1, shareB) {
		t.Fatal("different tenants produced same OPRF share")
	}
}

func TestWrapTenantKEK_InvalidKeySize(t *testing.T) {
	rootKEK, _ := GenerateKey()
	shortKEK := []byte("short")
	_, err := WrapTenantKEK(rootKEK, shortKEK, "tenant-a")
	if err == nil {
		t.Fatal("expected error for short KEK")
	}
}

func TestUnwrapTenantKEK_InvalidWrapped(t *testing.T) {
	rootKEK, _ := GenerateKey()
	_, err := UnwrapTenantKEK(rootKEK, []byte("invalid"), "tenant-a")
	if err == nil {
		t.Fatal("expected error for invalid wrapped KEK")
	}
}

func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
