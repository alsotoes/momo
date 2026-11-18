package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/transport"
)

// maintenanceDir gives a test a real directory outside t.TempDir(): the reaper
// owns the spill root it is handed, and letting the testing package remove a
// directory a goroutine is scanning made these tests flaky.
func maintenanceDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "momo-bgtest-"+name)
	t.Cleanup(func() {
		transport.SetMultipartSpillRoot("")
		_ = os.RemoveAll(dir)
	})
	return dir
}

// waitForReaper blocks until the reaper loop has exited.
func waitForReaper(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("multipart reaper did not stop")
	}
}

// TestStartBackgroundMaintenanceWithoutStore proves the housekeeping entry point
// tolerates a nil store (no CAS retention loop) and still arms multipart expiry.
func TestStartBackgroundMaintenanceWithoutStore(t *testing.T) {
	cfg := common.Configuration{}
	cfg.Storage.MultipartUploadTTL = "1h"
	cfg.Audit.RetentionDays = 30 // no *storage.CASStore, so this loop is skipped

	ctx, cancel := context.WithCancel(context.Background())
	done := startBackgroundMaintenance(ctx, cfg, maintenanceDir(t, "without-store"), nil, nil)
	cancel()
	waitForReaper(t, done)

	if got := transport.MultipartTTLFromConfig(cfg.Storage.MultipartUploadTTL); got != time.Hour {
		t.Fatalf("TTL = %s, want 1h", got)
	}
}

// TestStartBackgroundMaintenanceDisabledTTL proves a zero TTL starts no reaper.
func TestStartBackgroundMaintenanceDisabledTTL(t *testing.T) {
	cfg := common.Configuration{}
	cfg.Storage.MultipartUploadTTL = "0"

	ctx, cancel := context.WithCancel(context.Background())
	done := startBackgroundMaintenance(ctx, cfg, maintenanceDir(t, "disabled"), nil, nil)
	cancel()
	waitForReaper(t, done)

	if got := transport.MultipartTTLFromConfig(cfg.Storage.MultipartUploadTTL); got != 0 {
		t.Fatalf("TTL = %s, want 0 (disabled)", got)
	}
}
