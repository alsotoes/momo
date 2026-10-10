package common

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

func TestSecretProviderChain_Fallback(t *testing.T) {
	os.Setenv("MOMO_TEST_TOKEN", "hit")
	defer os.Unsetenv("MOMO_TEST_TOKEN")

	// The first provider reports "not found"; the chain must fall through to env.
	chain := NewProviderChain(&errProvider{}, NewEnvProvider("MOMO_TEST_"))
	val, found, err := chain.GetSecret(context.Background(), "token")
	if err != nil || !found || val != "hit" {
		t.Fatalf("expected env fallback, got val=%q found=%v err=%v", val, found, err)
	}

	// Not found in any provider -> not found, no error.
	if _, found, err := chain.GetSecret(context.Background(), "ABSENT_SECRET"); err != nil || found {
		t.Fatalf("expected not-found, got found=%v err=%v", found, err)
	}
}

func TestRotationManager_AuditRecorded(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer db.Close()

	rm := NewRotationManager(kr, &errProvider{}, time.Hour)
	rm.SetAuditDB(db)

	if err := rm.Rotate(context.Background(), "encryption"); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	var count int
	if err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("audit_log"))
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, _ []byte) error { count++; return nil })
	}); err != nil {
		t.Fatalf("read audit_log: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least one rotation audit entry")
	}
}

func TestRotationManager_OldKeyRetained(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer db.Close()

	ctx := context.Background()
	oldID, err := kr.StoreKey(ctx, "encryption", []byte("0123456789abcdef0123456789abcdef"), "AES-256-GCM", "", KeyStatusActive, 0)
	if err != nil {
		t.Fatalf("StoreKey: %v", err)
	}

	rm := NewRotationManager(kr, &errProvider{}, time.Hour)
	if err := rm.Rotate(ctx, "encryption"); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	// The old key must still be retrievable (readable for decryption) after rotation.
	old, err := kr.GetKey(ctx, oldID)
	if err != nil {
		t.Fatalf("old key should remain retrievable after rotation: %v", err)
	}
	if old.Status != KeyStatusRetired {
		t.Fatalf("expected old key retired, got %s", old.Status)
	}

	// And the active key must differ from the old one.
	newID, _, err := kr.GetActiveKey(ctx, "encryption", "")
	if err != nil {
		t.Fatalf("GetActiveKey: %v", err)
	}
	if newID == oldID {
		t.Fatal("expected a new active key after rotation")
	}
}

func TestRotationManager_AuditError(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer db.Close()

	// A separate, closed audit DB forces AuditRotation to fail; rotation must
	// still succeed (auditing is best-effort).
	auditDB, err := bbolt.Open(filepath.Join(t.TempDir(), "audit.db"), 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	auditDB.Close()

	rm := NewRotationManager(kr, &errProvider{}, time.Hour)
	rm.SetAuditDB(auditDB)

	if err := rm.Rotate(context.Background(), "encryption"); err != nil {
		t.Fatalf("Rotate should succeed despite an audit failure: %v", err)
	}
}
