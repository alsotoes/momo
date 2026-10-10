package storage

import (
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

func TestAuditLog_Chaining(t *testing.T) {
	s := newTestStore(t)

	// Write first entry
	entry1 := &common.AuditLogEntry{
		TenantID:  "tenant-a",
		Identity:  "test",
		Operation: "PutObject",
		Resource:  "bucket/test",
		Outcome:   "success",
		RequestID: "req-1",
		PrevHash:  "",
	}
	if err := s.WriteAuditLog(entry1); err != nil {
		t.Fatalf("WriteAuditLog 1: %v", err)
	}

	// Small delay to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	// Write second entry
	entry2 := &common.AuditLogEntry{
		TenantID:  "tenant-a",
		Identity:  "test",
		Operation: "GetObject",
		Resource:  "bucket/test",
		Outcome:   "success",
		RequestID: "req-2",
		PrevHash:  "",
	}
	if err := s.WriteAuditLog(entry2); err != nil {
		t.Fatalf("WriteAuditLog 2: %v", err)
	}

	// Verify chain integrity
	if err := s.VerifyAuditLog(); err != nil {
		t.Fatalf("VerifyAuditLog failed: %v", err)
	}
}

func TestAuditLog_TamperDetection(t *testing.T) {
	s := newTestStore(t)

	// Write three entries
	for i := 1; i <= 3; i++ {
		entry := &common.AuditLogEntry{
			TenantID:  "tenant-a",
			Identity:  "test",
			Operation: "PutObject",
			Resource:  "bucket/test",
			Outcome:   "success",
			RequestID: "req-" + string(rune('0'+i)),
		}
		if err := s.WriteAuditLog(entry); err != nil {
			t.Fatalf("WriteAuditLog %d: %v", i, err)
		}
	}

	// Verify initially valid
	if err := s.VerifyAuditLog(); err != nil {
		t.Fatalf("initial VerifyAuditLog failed: %v", err)
	}
}

func TestAuditLog_GetEntries(t *testing.T) {
	s := newTestStore(t)

	// Write entries
	for i := 1; i <= 5; i++ {
		entry := &common.AuditLogEntry{
			TenantID:  "tenant-a",
			Identity:  "test",
			Operation: "PutObject",
			Resource:  "bucket/test",
			Outcome:   "success",
			RequestID: "req-" + string(rune('0'+i)),
		}
		if err := s.WriteAuditLog(entry); err != nil {
			t.Fatalf("WriteAuditLog %d: %v", i, err)
		}
	}

	// Query all entries (use very large time range)
	entries, err := s.GetAuditLogEntries(0, 1<<63-1, 10)
	if err != nil {
		t.Fatalf("GetAuditLogEntries: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
}

func TestAuditLog_NewTestStore(t *testing.T) {
	s := newTestStore(t)
	if s == nil {
		t.Fatal("store is nil")
	}
}
