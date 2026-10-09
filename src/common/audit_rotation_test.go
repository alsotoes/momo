package common

import (
	"context"
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

func TestAuditRotation_NilDB(t *testing.T) {
	if err := AuditRotation(context.Background(), nil, AuditRotationEntry{Purpose: "encryption", OldKeyID: "old", NewKeyID: "new", Operator: "system", Trigger: "manual", Success: true}); err != nil {
		t.Fatalf("AuditRotation(nil db) = %v, want nil", err)
	}
}

func TestAuditRotation_WritesEntry(t *testing.T) {
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "audit.db"), 0600, nil)
	if err != nil {
		t.Fatalf("open bbolt: %v", err)
	}
	defer db.Close()

	if err := AuditRotation(context.Background(), db, AuditRotationEntry{Purpose: "encryption", OldKeyID: "old-1", NewKeyID: "new-2", Operator: "system", Trigger: "scheduled", Success: true}); err != nil {
		t.Fatalf("AuditRotation: %v", err)
	}

	var count int
	if err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("audit_log"))
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, _ []byte) error {
			count++
			return nil
		})
	}); err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 audit entry, got %d", count)
	}
}
