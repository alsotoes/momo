package server

import (
	"context"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"go.uber.org/goleak"
)

func TestStartAuditPruneLoop_NoopPaths(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx := context.Background()

	// Nil store exits immediately.
	done := startAuditPruneLoop(ctx, nil, nil, 365)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("nil store did not exit")
	}

	// Non-positive retention never runs.
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	done = startAuditPruneLoop(ctx, store, nil, 0)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retentionDays=0 did not exit")
	}
}

func TestStartAuditPruneLoop_PrunesAtStartupAndStops(t *testing.T) {
	defer goleak.VerifyNone(t)
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// One stale entry that a 1-day retention must remove on the first pass.
	if err := store.WriteAuditLog(&common.AuditLogEntry{
		TenantID: "t", Operation: "PutObject", Outcome: "success",
	}); err != nil {
		t.Fatalf("WriteAuditLog: %v", err)
	}
	// Backdate by rewriting with an old timestamp is not exposed, so assert on
	// the run counter instead: the loop must have run once.
	collector := NewMetricsCollector()
	ctx, cancel := context.WithCancel(context.Background())
	done := startAuditPruneLoop(ctx, store, collector, 365)

	// The startup pass is synchronous within the goroutine; wait for it to run.
	deadline := time.Now().Add(2 * time.Second)
	for collector.auditPruneRuns.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := collector.auditPruneRuns.Load(); got != 1 {
		cancel()
		t.Fatalf("prune runs = %d, want 1 after startup", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit on cancel")
	}
}
