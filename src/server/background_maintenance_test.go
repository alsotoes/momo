package server

import (
	"context"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/transport"
)

// TestStartBackgroundMaintenanceWithoutStore proves the housekeeping entry point
// tolerates a nil store (no CAS retention loop) and still arms multipart expiry.
func TestStartBackgroundMaintenanceWithoutStore(t *testing.T) {
	cfg := common.Configuration{}
	cfg.Storage.MultipartUploadTTL = "1h"
	cfg.Audit.RetentionDays = 30 // no *storage.CASStore, so this loop is skipped

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Must not panic: the audit loop needs a CASStore, multipart expiry does not.
	startBackgroundMaintenance(ctx, cfg, t.TempDir(), nil, nil)

	if got := transport.MultipartTTLFromConfig(cfg.Storage.MultipartUploadTTL); got != time.Hour {
		t.Fatalf("spill root TTL = %s, want 1h", got)
	}
}

// TestStartBackgroundMaintenanceDisabledTTL proves a zero TTL starts no reaper.
func TestStartBackgroundMaintenanceDisabledTTL(t *testing.T) {
	cfg := common.Configuration{}
	cfg.Storage.MultipartUploadTTL = "0"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startBackgroundMaintenance(ctx, cfg, t.TempDir(), nil, nil)

	if got := transport.MultipartTTLFromConfig(cfg.Storage.MultipartUploadTTL); got != 0 {
		t.Fatalf("TTL = %s, want 0 (disabled)", got)
	}
}
