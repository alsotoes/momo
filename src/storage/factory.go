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
	s, _, err := buildCAS(cfg, daemon, encKeyHex)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// NewStoreWithRebuild is like NewStore but additionally arms the R2 self-heal
// rebuild loop and survivor-set degraded-read fallback (Rule 74, #930) against
// the given cluster seam. A nil src leaves degraded read and the rebuild loop
// inert (single-node / legacy behavior); target is the replica count the loop
// restores (the daemon's configured replication factor). Interval, worker count
// and degraded-read toggle come from cfg (R2-G1).
func NewStoreWithRebuild(cfg common.ConfigurationStorage, daemon *common.Daemon, encKeyHex string, src RebuildSource, target int) (Store, error) {
	s, _, err := buildCAS(cfg, daemon, encKeyHex)
	if err != nil {
		return nil, err
	}
	if src != nil && target > 0 {
		s.StartRebuild(RebuildConfig{
			Interval:     time.Duration(cfg.RebuildInterval) * time.Second,
			Workers:      cfg.RebuildWorkers,
			Target:       target,
			Source:       src,
			DegradedRead: cfg.DegradedRead,
		})
	}
	return s, nil
}

// buildCAS constructs the CAS store, starts GC/scrub, and returns ownership of
// the underlying blobs so the caller can close them on downstream failure.
func buildCAS(cfg common.ConfigurationStorage, daemon *common.Daemon, encKeyHex string) (*CASStore, BlobStore, error) {
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
		return nil, nil, fmt.Errorf("unsupported storage backend %q: %w", cfg.Backend, syscall.EINVAL)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize %s blob store: %w", cfg.Backend, err)
	}

	// Wrap with server-side encryption at rest when enabled.
	if encKeyHex != "" {
		encBlobs, encErr := NewEncryptedBlobStore(blobs, encKeyHex)
		if encErr != nil {
			blobs.Close()
			return nil, nil, fmt.Errorf("failed to initialize encrypted blob store: %w", encErr)
		}
		blobs = encBlobs
	}

	s, err := newCASStore(daemon.Data, blobs)
	if err != nil {
		return nil, blobs, err
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

	return s, blobs, nil
}
