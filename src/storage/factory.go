package storage

import (
	"fmt"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// NewStore creates a Store backed by the configured backend type.
// The backend is selected from cfg.Backend:
//   - "" or "local" (default): local filesystem with tiered directory layout
//   - "nfs": local filesystem on an NFS mount (functionally identical to "local")
//   - "s3": S3-compatible remote storage (Phase 4)
//   - "raw": raw block device (Phase 5)
//
// Garbage collection is started automatically with the configured intervals.
// The bbolt metadata database is always stored locally in daemon.Data.
func NewStore(cfg common.ConfigurationStorage, daemon *common.Daemon) (Store, error) {
	var blobs BlobStore
	var err error

	switch cfg.Backend {
	case "", "local", "nfs":
		blobs, err = NewLocalBlobStore(daemon.Data)
	case "s3":
		return nil, fmt.Errorf("storage backend %q is not yet implemented: %w", cfg.Backend, syscall.ENOSYS)
	case "raw":
		return nil, fmt.Errorf("storage backend %q is not yet implemented: %w", cfg.Backend, syscall.ENOSYS)
	default:
		return nil, fmt.Errorf("unsupported storage backend %q: %w", cfg.Backend, syscall.EINVAL)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to initialize %s blob store: %w", cfg.Backend, err)
	}

	s, err := newCASStore(daemon.Data, blobs)
	if err != nil {
		blobs.Close()
		return nil, err
	}

	gcInterval := time.Duration(cfg.GCInterval) * time.Second
	if gcInterval <= 0 {
		gcInterval = 5 * time.Minute
	}
	tombstoneRetention := time.Duration(cfg.TombstoneRetention) * time.Second
	if tombstoneRetention <= 0 {
		tombstoneRetention = 24 * time.Hour
	}
	s.StartGC(GCConfig{
		Interval:           gcInterval,
		TombstoneRetention: tombstoneRetention,
	})

	return s, nil
}
