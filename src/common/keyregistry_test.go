package common

import (
	"context"
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

func TestKeyRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := bbolt.Open(dbPath, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open bbolt: %v", err)
	}
	defer db.Close()

	kr, err := NewKeyRegistry(db)
	if err != nil {
		t.Fatalf("NewKeyRegistry failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Test 1: StoreKey and GetActiveKey
	keyID, err := kr.StoreKey(ctx, "encryption", []byte("test-key-material"), "AES-256-GCM", "", KeyStatusActive, 0)
	if err != nil {
		t.Fatalf("StoreKey failed: %v", err)
	}

	keyID2, material, err := kr.GetActiveKey(ctx, "encryption", "")
	if err != nil {
		t.Fatalf("GetActiveKey failed: %v", err)
	}
	if keyID2 != keyID {
		t.Fatalf("GetActiveKey returned wrong keyID: %s != %s", keyID2, keyID)
	}
	_ = material

	// Test 2: SetActive demotes old key
	keyID3, err := kr.StoreKey(ctx, "encryption", []byte("new-material"), "AES-256-GCM", "", KeyStatusPendingRotation, 0)
	if err != nil {
		t.Fatalf("StoreKey v2 failed: %v", err)
	}

	err = kr.SetActive(ctx, keyID3)
	if err != nil {
		t.Fatalf("SetActive failed: %v", err)
	}

	activeID, _, err := kr.GetActiveKey(ctx, "encryption", "")
	if err != nil {
		t.Fatalf("GetActiveKey after SetActive failed: %v", err)
	}
	if activeID != keyID3 {
		t.Fatalf("active key should be %s, got %s", keyID3, activeID)
	}

	// Verify old key is retired
	oldEntry, _ := kr.GetKey(ctx, keyID)
	if oldEntry.Status != KeyStatusRetired {
		t.Fatalf("old key should be retired, got %s", oldEntry.Status)
	}

	// Test 3: MarkRetired
	keyID4, _ := kr.StoreKey(ctx, "auth", []byte("auth-key"), "HMAC-SHA256", "", KeyStatusActive, 0)
	err = kr.MarkRetired(ctx, keyID4)
	if err != nil {
		t.Fatalf("MarkRetired failed: %v", err)
	}
	entry, _ := kr.GetKey(ctx, keyID4)
	if entry.Status != KeyStatusRetired {
		t.Fatalf("key should be retired, got %s", entry.Status)
	}
	if entry.RotatedAt == nil {
		t.Fatal("RotatedAt should be set after MarkRetired")
	}

	// Test 4: MarkCompromised
	_, _ = kr.StoreKey(ctx, "e2ee", []byte("e2ee-key"), "AES-256-GCM", "", KeyStatusActive, 0)
	err = kr.MarkCompromised(ctx, keyID4)
	if err != nil {
		t.Fatalf("MarkCompromised failed: %v", err)
	}
	entry5, _ := kr.GetKey(ctx, keyID4)
	if entry5.Status != KeyStatusCompromised {
		t.Fatalf("key should be compromised, got %s", entry5.Status)
	}

	// Test 5: ListVersions
	versions, err := kr.ListVersions(ctx, "encryption")
	if err != nil {
		t.Fatalf("ListVersions failed: %v", err)
	}
	if len(versions) < 2 {
		t.Fatalf("expected at least 2 versions, got %d", len(versions))
	}

	// Test 6: GetKey
	entry6, err := kr.GetKey(ctx, keyID)
	if err != nil {
		t.Fatalf("GetKey failed: %v", err)
	}
	if entry6.KeyID != keyID {
		t.Fatalf("GetKey returned wrong entry")
	}

	// Test 7: GetActiveKeyForPurpose (with tenant)
	keyID6, _ := kr.StoreKey(ctx, "auth", []byte("tenant-key"), "HMAC-SHA256", "tenant1", KeyStatusActive, 0)
	activeTenantID, _, err := kr.GetActiveKey(ctx, "auth", "tenant1")
	if err != nil {
		t.Fatalf("GetActiveKeyForPurpose failed: %v", err)
	}
	if activeTenantID != keyID6 {
		t.Fatalf("expected %s, got %s", keyID6, activeTenantID)
	}

	// Test 7: Change hook registration
	hookCalled := false
	kr.RegisterChangeHook(func(entry KeyEntry) {
		hookCalled = true
	})
	_, err = kr.StoreKey(ctx, "oprf", []byte("oprf-key"), "OPRF-Shamir", "", KeyStatusActive, 0)
	if err != nil {
		t.Fatalf("StoreKey after hook failed: %v", err)
	}
	if !hookCalled {
		t.Fatal("change hook was not called")
	}
}

func TestKeyRegistry_EdgeCases(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := bbolt.Open(dbPath, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open bbolt: %v", err)
	}
	defer db.Close()

	kr, err := NewKeyRegistry(db)
	if err != nil {
		t.Fatalf("NewKeyRegistry failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Test: GetActiveKey returns nil when no active key
	_, _, err = kr.GetActiveKey(ctx, "nonexistent", "")
	if err != nil {
		t.Fatalf("GetActiveKey should not error: %v", err)
	}

	// Test: GetKey returns nil for missing key
	_, err = kr.GetKey(ctx, "nonexistent-key")
	if err != nil {
		t.Fatalf("GetKey should not error for missing key")
	}

	// Test: SetActive on non-existent key
	err = kr.SetActive(ctx, "nonexistent-key")
	if err == nil {
		t.Fatal("SetActive should error for non-existent key")
	}

	// Test: MarkRetired on non-existent key
	err = kr.MarkRetired(ctx, "nonexistent-key")
	if err == nil {
		t.Fatal("MarkRetired should error for non-existent key")
	}

	// Test: MarkCompromised on non-existent key
	err = kr.MarkCompromised(ctx, "nonexistent-key")
	if err == nil {
		t.Fatal("MarkCompromised should error for non-existent key")
	}

	// Test: StoreKey with empty purpose
	_, err = kr.StoreKey(ctx, "", []byte("test"), "AES-256-GCM", "", KeyStatusActive, 0)
	if err == nil {
		t.Fatal("StoreKey should error for empty purpose")
	}
}
