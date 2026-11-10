package storage

import (
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	"go.etcd.io/bbolt"

	"github.com/alsotoes/momo/src/common"
)

// writeAudit writes one entry and returns the time immediately after it was
// stored, usable as a prune cutoff that excludes it and everything before.
func writeAudit(t *testing.T, s *CASStore, op string) time.Time {
	t.Helper()
	if err := s.WriteAuditLog(&common.AuditLogEntry{
		TenantID:  "tenant-a",
		Identity:  "test",
		Operation: op,
		Resource:  "bucket/test",
		Outcome:   "success",
	}); err != nil {
		t.Fatalf("WriteAuditLog(%s): %v", op, err)
	}
	time.Sleep(2 * time.Millisecond) // distinct nanosecond keys
	return time.Now()
}

func countAuditEntries(t *testing.T, s *CASStore) int {
	t.Helper()
	n := 0
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketAuditLog)
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, _ []byte) error { n++; return nil })
	})
	if err != nil {
		t.Fatalf("count audit entries: %v", err)
	}
	return n
}

func TestPruneAuditLog_PartialKeepsChainValid(t *testing.T) {
	s := newTestStore(t)
	writeAudit(t, s, "PutObject")
	cutoff := writeAudit(t, s, "GetObject")
	writeAudit(t, s, "DeleteObject")

	if n := countAuditEntries(t, s); n != 3 {
		t.Fatalf("pre-prune entries = %d, want 3", n)
	}

	pruned, err := s.PruneAuditLog(cutoff)
	if err != nil {
		t.Fatalf("PruneAuditLog: %v", err)
	}
	if pruned != 2 {
		t.Fatalf("pruned = %d, want 2", pruned)
	}
	if n := countAuditEntries(t, s); n != 1 {
		t.Fatalf("post-prune entries = %d, want 1", n)
	}
	if err := s.VerifyAuditLog(); err != nil {
		t.Fatalf("VerifyAuditLog after partial prune: %v", err)
	}
}

func TestPruneAuditLog_AllEntries(t *testing.T) {
	s := newTestStore(t)
	writeAudit(t, s, "PutObject")
	writeAudit(t, s, "GetObject")
	cutoff := time.Now().Add(time.Hour)

	pruned, err := s.PruneAuditLog(cutoff)
	if err != nil {
		t.Fatalf("PruneAuditLog: %v", err)
	}
	if pruned != 2 {
		t.Fatalf("pruned = %d, want 2", pruned)
	}
	if n := countAuditEntries(t, s); n != 0 {
		t.Fatalf("post-prune entries = %d, want 0", n)
	}
	if err := s.VerifyAuditLog(); err != nil {
		t.Fatalf("VerifyAuditLog on empty log: %v", err)
	}
}

func TestPruneAuditLog_NewWritesChainOnAnchor(t *testing.T) {
	s := newTestStore(t)
	writeAudit(t, s, "PutObject")
	cutoff := writeAudit(t, s, "GetObject")
	writeAudit(t, s, "DeleteObject")

	if _, err := s.PruneAuditLog(cutoff); err != nil {
		t.Fatalf("PruneAuditLog: %v", err)
	}
	// A write after pruning must chain onto the retained entry, and the log
	// must remain verifiable end to end.
	writeAudit(t, s, "PutObject")
	if err := s.VerifyAuditLog(); err != nil {
		t.Fatalf("VerifyAuditLog after post-prune write: %v", err)
	}
}

func TestPruneAuditLog_NoopPaths(t *testing.T) {
	s := newTestStore(t)
	writeAudit(t, s, "PutObject")

	if pruned, err := s.PruneAuditLog(time.Time{}); err != nil || pruned != 0 {
		t.Fatalf("zero cutoff: pruned=%d err=%v, want 0/nil", pruned, err)
	}
	past := time.Now().AddDate(-1, 0, 0)
	if pruned, err := s.PruneAuditLog(past); err != nil || pruned != 0 {
		t.Fatalf("past cutoff: pruned=%d err=%v, want 0/nil", pruned, err)
	}
	if n := countAuditEntries(t, s); n != 1 {
		t.Fatalf("entries = %d, want 1 (nothing pruned)", n)
	}
}

func TestPruneAuditLog_TamperStillDetectedWithinWindow(t *testing.T) {
	s := newTestStore(t)
	writeAudit(t, s, "PutObject")
	cutoff := writeAudit(t, s, "GetObject")
	writeAudit(t, s, "DeleteObject")

	if _, err := s.PruneAuditLog(cutoff); err != nil {
		t.Fatalf("PruneAuditLog: %v", err)
	}

	// Tamper with the retained entry without recomputing its hash.
	err := s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketAuditLog)
		k, v := b.Cursor().First()
		if k == nil {
			return nil
		}
		var entry common.AuditLogEntry
		if err := json.Unmarshal(v, &entry); err != nil {
			return err
		}
		entry.Outcome = "failure"
		data, err := json.Marshal(&entry)
		if err != nil {
			return err
		}
		var key [8]byte
		binary.BigEndian.PutUint64(key[:], uint64(entry.Timestamp))
		return b.Put(key[:], data)
	})
	if err != nil {
		t.Fatalf("tamper: %v", err)
	}

	if err := s.VerifyAuditLog(); err == nil {
		t.Fatal("VerifyAuditLog must detect tampering inside the retained window")
	}
}

func TestPruneAuditLog_AnchorRecordedInMeta(t *testing.T) {
	s := newTestStore(t)
	writeAudit(t, s, "PutObject")
	cutoff := writeAudit(t, s, "GetObject")
	writeAudit(t, s, "DeleteObject")

	if _, err := s.PruneAuditLog(cutoff); err != nil {
		t.Fatalf("PruneAuditLog: %v", err)
	}

	var anchor string
	err := s.db.View(func(tx *bbolt.Tx) error {
		anchor = string(tx.Bucket(bucketAuditMeta).Get(auditPruneAnchorKey))
		return nil
	})
	if err != nil {
		t.Fatalf("read anchor: %v", err)
	}
	if anchor == "" {
		t.Fatal("prune anchor not recorded")
	}
}
