package common

import (
	"context"
	"errors"
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

func TestRotationManager_RotateLeavesRegistryUntouched(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer db.Close()

	ctx := context.Background()
	oldID, err := kr.StoreKey(ctx, "encryption", []byte("0123456789abcdef0123456789abcdef"), "AES-256-GCM", "", KeyStatusActive, 0)
	if err != nil {
		t.Fatalf("StoreKey: %v", err)
	}

	rm := NewRotationManager(kr, &errProvider{}, time.Hour)
	rm.SetAuditDB(db)

	if err := rm.Rotate(ctx, "encryption"); !errors.Is(err, ErrRotationNotWired) {
		t.Fatalf("expected ErrRotationNotWired, got %v", err)
	}

	// The configured key must still be the active one: a refused rotation must
	// not demote it, because nothing would ever decrypt the old data again.
	activeID, _, err := kr.GetActiveKeyForPurpose(ctx, "encryption")
	if err != nil {
		t.Fatalf("GetActiveKeyForPurpose: %v", err)
	}
	if activeID != oldID {
		t.Fatalf("refused rotation changed the active key: %q -> %q", oldID, activeID)
	}
	old, err := kr.GetKey(ctx, oldID)
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if old.Status != KeyStatusActive {
		t.Fatalf("refused rotation changed the key status to %s", old.Status)
	}

	// And the refusal is still recorded.
	count := 0
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
		t.Fatal("expected a rotation audit entry")
	}
}

func TestRotationManager_AuditFailureDoesNotMaskRefusal(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer db.Close()

	// A separate, closed audit DB forces AuditRotation to fail; the refusal
	// must still be reported as ErrRotationNotWired, not as a panic or a
	// misleading success.
	auditDB, err := bbolt.Open(filepath.Join(t.TempDir(), "audit.db"), 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	auditDB.Close()

	rm := NewRotationManager(kr, &errProvider{}, time.Hour)
	rm.SetAuditDB(auditDB)

	if err := rm.Rotate(context.Background(), "encryption"); !errors.Is(err, ErrRotationNotWired) {
		t.Fatalf("expected ErrRotationNotWired despite audit failure, got %v", err)
	}
}
