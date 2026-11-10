package server

import (
	"context"
	"log"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

// auditPruneInterval is how often the audit log is pruned. Retention is
// expressed in days, so a daily pass keeps the log within one interval of the
// configured window without scanning it more often than necessary (#1307).
const auditPruneInterval = 24 * time.Hour

// pruneAuditOnce runs one retention pass: delete entries older than
// retentionDays and record the outcome. Kept separate from the loop so both
// stay within the complexity budget (Rule 345).
func pruneAuditOnce(store *storage.CASStore, collector *MetricsCollector, retentionDays int) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	pruned, err := store.PruneAuditLog(cutoff)
	if collector != nil {
		collector.IncAuditPruneRun()
	}
	if err != nil {
		log.Printf("⚠️ audit retention: prune failed: %v", err)
		return
	}
	if pruned == 0 {
		return
	}
	if collector != nil {
		collector.AddAuditPruned(uint64(pruned))
	}
	log.Printf("AUDIT: pruned %d audit entries older than %d days", pruned, retentionDays)
}

// startAuditPruneLoop enforces the [audit] retention_days policy: once at
// startup (so a long-down node catches up immediately) and then every
// auditPruneInterval. It returns a channel closed when the loop exits, so the
// caller can wait for a clean shutdown in tests.
func startAuditPruneLoop(ctx context.Context, store *storage.CASStore, collector *MetricsCollector, retentionDays int) <-chan struct{} {
	done := make(chan struct{})
	if store == nil || retentionDays < 1 {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		defer common.RecoverClose("audit prune loop", nil) //nolint:staticcheck // log-only guard
		pruneAuditOnce(store, collector, retentionDays)
		ticker := time.NewTicker(auditPruneInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pruneAuditOnce(store, collector, retentionDays)
			}
		}
	}()
	return done
}
