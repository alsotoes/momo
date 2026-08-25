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
// When encKeyHex is non-empty, the blob store is wrapped with
// EncryptedBlobStore (AES-GCM-256 server-side encryption at rest).
// Garbage collection is started automatically with the configured intervals.
// The bbolt metadata database is always stored locally in daemon.Data.
func NewStore(cfg common.ConfigurationStorage, daemon *common.Daemon, encKeyHex string) (Store, error) {
	var blobs BlobStore
	var err error

	switch cfg.Backend {
	case "", common.BackendLocal, common.BackendNFS:
		blobs, err = NewLocalBlobStore(daemon.Data)
	case common.BackendS3:
		blobs, err = NewS3BlobStore(cfg)
	case common.BackendRaw:
		blobs, err = NewRawBlobStore(cfg, daemon)
	default:
		return nil, fmt.Errorf("unsupported storage backend %q: %w", cfg.Backend, syscall.EINVAL)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to initialize %s blob store: %w", cfg.Backend, err)
	}

	// Wrap with server-side encryption at rest when enabled.
	if encKeyHex != "" {
		encBlobs, encErr := NewEncryptedBlobStore(blobs, encKeyHex)
		if encErr != nil {
			blobs.Close()
			return nil, fmt.Errorf("failed to initialize encrypted blob store: %w", encErr)
		}
		blobs = encBlobs
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

	scrubInterval := time.Duration(cfg.ScrubInterval) * time.Second
	if scrubInterval <= 0 {
		scrubInterval = time.Hour
	}
	s.VerifyOnRead = cfg.VerifyOnRead
	s.StartScrub(ScrubConfig{Interval: scrubInterval})

	return s, nil
}
