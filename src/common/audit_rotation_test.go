package common

import (
	"context"
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

func TestAuditRotation_NilDB(t *testing.T) {
	if err := AuditRotation(context.Background(), nil, "encryption", "old", "new", "system", "manual", true, ""); err != nil {
		t.Fatalf("AuditRotation(nil db) = %v, want nil", err)
	}
}

func TestAuditRotation_WritesEntry(t *testing.T) {
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "audit.db"), 0600, nil)
	if err != nil {
		t.Fatalf("open bbolt: %v", err)
	}
	defer db.Close()

	if err := AuditRotation(context.Background(), db, "encryption", "old-1", "new-2", "system", "scheduled", true, ""); err != nil {
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
