package server

import (
	"context"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
)

// startBackgroundMaintenance launches the periodic housekeeping loops for this
// daemon: audit-log retention and expiry of incomplete S3 multipart uploads.
// Keeping them behind one call keeps the daemon constructor readable.
func startBackgroundMaintenance(ctx context.Context, cfg common.Configuration, dataDir string, store storage.Store, collector *MetricsCollector) {
	if cs, ok := store.(*storage.CASStore); ok && cfg.Audit.RetentionDays > 0 {
		startAuditPruneLoop(ctx, cs, collector, cfg.Audit.RetentionDays)
	}
	// Issue #1361: an abandoned multipart upload otherwise pins its spooled
	// parts for the life of the daemon (and a daemon killed mid-upload leaves
	// the directory behind entirely). Parts live under the daemon's data dir, not
	// a shared temp directory, so the sweep only ever deletes what it created.
	transport.SetMultipartSpillRoot(dataDir)
	transport.StartMultipartReaper(ctx, transport.MultipartTTLFromConfig(cfg.Storage.MultipartUploadTTL))
}
