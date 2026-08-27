package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"go.etcd.io/bbolt"
)

// Peer identifies a storage daemon in the cluster replica set (R2, #930).
type Peer struct {
	ID int
	// Domain is the R1 failure-domain label (rack/zone/DC) of the peer; ""
	// means unclassified.
	Domain string
}

// RebuildSource is the Rule 74 seam that gives the self-heal loop and the
// degraded-read path their cluster view WITHOUT dragging transport, CRUSH
// placement, or peer protocol details into the storage core. The daemon layer
// wires a real implementation (CRUSH placement + client.Download + replication
// push); tests inject in-memory fakes. Nil disables degraded-read fallback and
// the rebuild loop, preserving single-node / legacy behavior.
type RebuildSource interface {
	// Survivors returns the replica-set members OTHER than this node that
	// currently hold a copy of blob hash whose bytes match the content hash.
	// The list is advisory: verify-before-use is enforced by Fetch.
	Survivors(hash string) ([]Peer, error)
	// Fetch opens a verify-before-use stream of blob hash served by survivor:
	// the returned reader MUST re-derive the content hash and fail the read if
	// the bytes are corrupted, so corrupt bytes are never stored or propagated
	// (R2-C4, #930). The reader must be closed by the caller.
	Fetch(hash string, survivor Peer) (io.ReadCloser, error)
	// Restore pushes already-verified blob bytes to every replica-set member
	// that does not currently hold a verified copy, preferring R1
	// failure-domain spread for new placements (R2-C5). The source owns
	// multi-target fan-out/buffering; content is pre-verified by the caller.
	Restore(hash string, content io.Reader) error
}

// RebuildConfig configures the background self-heal rebuild loop (R2, #930).
type RebuildConfig struct {
	// Interval is how often the loop passes over referenced blobs.
	Interval time.Duration
	// Workers bounds concurrent blob repairs per pass (R2-C6).
	Workers int
	// Target is the replica count the loop restores (R2-C5).
	Target int
	// Source supplies cluster-awareness. Nil makes the loop a no-op and leaves
	// degraded read disabled.
	Source RebuildSource
	// DegradedRead arms the survivor-set read fallback in Get (R2-C1).
	DegradedRead bool
}

// DefaultRebuildConfig returns a sensible default for the rebuild loop.
func DefaultRebuildConfig() RebuildConfig {
	return RebuildConfig{
		Interval:     5 * time.Minute,
		Workers:      4,
		Target:       3,
		DegradedRead: true,
	}
}

// StartRebuild launches the background self-heal rebuild goroutine. It is safe
// to call at most once per CASStore instance — the sync.Once guard makes
// repeated invocations no-ops (mirroring StartScrub/StartGC). Starting the loop
// also arms the survivor-set degraded-read fallback in Get (R2-C1).
func (s *CASStore) StartRebuild(cfg RebuildConfig) {
	s.rebuildOnce.Do(func() {
		// Arm the cluster seam + degraded-read synchronously so Get observes the
		// policy the moment StartRebuild returns (mirrors VerifyOnRead wiring).
		s.rebuildSource = cfg.Source
		s.rebuildTarget = cfg.Target
		s.degradedRead = cfg.DegradedRead
		s.rebuildStarted.Store(1)
		s.rebuildWG.Add(1)
		go s.rebuildLoop(cfg)
	})
}

func (s *CASStore) rebuildLoop(cfg RebuildConfig) {
	defer s.rebuildWG.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("STORAGE REBUILD: rebuildLoop panic recovered: %v", r)
		}
	}()

	if cfg.Source == nil {
		return
	}

	interval := cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.rebuildDone:
			return
		case <-ticker.C:
			if err := s.runRebuild(cfg); err != nil {
				log.Printf("STORAGE REBUILD: pass error: %v", err)
			}
		}
	}
}

// runRebuild performs a single self-heal pass over the referenced blob set
// (R2-C3). Work is bounded by a worker pool so a large cluster cannot trigger a
// thundering-herd of concurrent repairs (R2-C6).
func (s *CASStore) runRebuild(cfg RebuildConfig) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("STORAGE REBUILD: runRebuild panic recovered: %v", r)
			err = fmt.Errorf("storage rebuild panic: %w", syscall.EIO)
		}
	}()

	hashes, err := s.rebuildSet()
	if err != nil {
		return err
	}

	workers := cfg.Workers
	if workers <= 0 {
		workers = 4
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for _, hash := range hashes {
		select {
		case <-s.rebuildDone:
			return nil
		default:
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(h string) {
			defer wg.Done()
			defer func() { <-sem }()
			s.repairBlob(h, cfg)
		}(hash)
	}
	wg.Wait()
	return nil
}

// rebuildSet is the repair working set: every referenced blob plus every
// quarantine-marked blob (mark-and-hold copies must be replaced after
// re-replication, R2-C2).
func (s *CASStore) rebuildSet() ([]string, error) {
	hashes, err := s.referencedBlobs()
	if err != nil {
		return nil, err
	}
	flagged, err := s.quarantinedHashes()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(hashes)+len(flagged))
	all := make([]string, 0, len(hashes)+len(flagged))
	for _, h := range hashes {
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		all = append(all, h)
	}
	for _, h := range flagged {
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		all = append(all, h)
	}
	return all, nil
}

// repairBlob restores a single blob to its target replica count from a
// verified survivor, respecting the source's R1 failure-domain placement. It
// never propagates unverified bytes (R2-C4): every restore stream is either a
// locally re-verified copy or a verified fetch.
func (s *CASStore) repairBlob(hash string, cfg RebuildConfig) {
	src := cfg.Source
	if src == nil || cfg.Target <= 0 {
		return
	}
	select {
	case <-s.rebuildDone:
		return
	default:
	}

	survivors, err := src.Survivors(hash)
	if err != nil {
		log.Printf("STORAGE REBUILD: survivors query for %s failed: %v", hash, err)
		return
	}
	localOK := s.localCopyVerified(hash)
	flagged := s.quarantined(hash)

	// Healthy when the verified replica count (survivors plus this node, if its
	// local copy verifies) meets the target and the local copy is not
	// quarantine-marked.
	verified := len(survivors)
	if localOK {
		verified++
	}
	if !flagged && verified >= cfg.Target {
		return
	}

	// Obtain verified bytes: prefer a verified local copy; otherwise restore
	// them from a remote verified survivor (verify-before-store, R2-C4).
	var stream io.ReadCloser
	if localOK {
		f, ferr := s.blobs.GetBlob(hash)
		if ferr == nil {
			stream = f
		}
	}
	if stream == nil {
		rc, ferr := s.fetchVerifiedToLocal(hash)
		if ferr != nil {
			log.Printf("STORAGE REBUILD: no verified survivor for %s: %v", hash, ferr)
			return
		}
		stream = rc
	}
	defer stream.Close()

	if err := src.Restore(hash, stream); err != nil {
		log.Printf("STORAGE REBUILD: restore of %s failed: %v", hash, err)
		return
	}
	_ = s.quarantineClear(hash)
	s.repairs.Add(1)
	log.Printf("AUDIT: STORAGE REBUILD restored blob %s to target %d replicas (from %d verified survivor(s))",
		hash, cfg.Target, len(survivors))
}

// localCopyVerified reports whether the local blob bytes re-derive to hash.
// Full re-hash is bounded-memory and cheap enough for the low-frequency loop.
func (s *CASStore) localCopyVerified(hash string) bool {
	rc, err := s.blobs.GetBlob(hash)
	if err != nil {
		return false
	}
	defer rc.Close()
	if rc == nil {
		return false
	}
	digest, err := common.HashReader(rc)
	if err != nil {
		return false
	}
	return digest == hash
}

// fetchVerifiedToLocal restores a verified local copy of hash from the first
// survivor that serves uncorrupted bytes, stores it locally (verify-before-
// store, R2-C4), and returns a local verified stream. A survivor whose bytes
// fail hashing is quarantine-marked locally and skipped. Returns ENOENT when no
// verified survivor exists. R2-C2: a mark-and-hold copy is replaced by the
// verified fetch, then the mark is cleared.
func (s *CASStore) fetchVerifiedToLocal(hash string) (io.ReadCloser, error) {
	src := s.rebuildSource
	if src == nil {
		return nil, syscall.ENOENT
	}
	survivors, err := src.Survivors(hash)
	if err != nil {
		return nil, fmt.Errorf("survivors query for %s: %w", hash, err)
	}
	for _, peer := range survivors {
		rc, err := src.Fetch(hash, peer)
		if err != nil {
			log.Printf("STORAGE REBUILD: fetch from survivor %d for %s failed: %v", peer.ID, hash, err)
			continue
		}
		ok := s.storeVerified(hash, rc)
		rc.Close()
		if !ok {
			log.Printf("AUDIT: STORAGE REBUILD: survivor %d for %s failed verification", peer.ID, hash)
			_ = s.quarantineMark(hash)
			continue
		}
		f, err := s.blobs.GetBlob(hash)
		if err != nil {
			return nil, err
		}
		s.verifier.MarkTrusted(hash)
		_ = s.quarantineClear(hash)
		return f, nil
	}
	return nil, syscall.ENOENT
}

// openVerifiedBlob opens the local blob for hash, applying the read policy
// (R2-C1, #930). When the local copy is missing or quarantine-marked and
// survivor-set degraded read is armed (source configured + DegradedRead), the
// read is served from the first verified survivor (repair-on-read) instead of
// failing. When degraded read is armed but the local copy is present and not
// marked, the stream is wrapped in a full verification that quarantine-marks
// the blob on mismatch so a later read (or the rebuild loop) restores it from a
// survivor. Returns ENOENT only when no verified copy exists in the placement.
func (s *CASStore) openVerifiedBlob(hash string) (io.ReadCloser, error) {
	flagged := s.quarantined(hash)
	f, err := s.blobs.GetBlob(hash)
	if err == nil && !flagged {
		if s.VerifyOnRead {
			if s.degradedRead && s.rebuildSource != nil {
				// Corruption at EOF must not be served silently: mark-and-hold so
				// the next read degrades to a survivor and the loop re-repairs.
				vr := newVerifyingReader(f, hash)
				vr.onCorrupt = func() {
					log.Printf("AUDIT: STORAGE degraded read: blob %s failed verification; mark-and-hold for self-heal", hash)
					_ = s.quarantineMark(hash)
				}
				return &verifyingReadCloser{verifyingReader: vr, underlying: f}, nil
			}
			return s.verifier.Verify(f, hash), nil
		}
		return f, nil
	}
	if err != nil && err != syscall.ENOENT && !os.IsNotExist(err) {
		return nil, err
	}

	// Local copy missing or untrustworthy: try survivor-set degraded read.
	if !s.degradedRead || s.rebuildSource == nil {
		return nil, syscall.ENOENT
	}
	restored, ferr := s.fetchVerifiedToLocal(hash)
	if ferr != nil {
		return nil, ferr
	}
	return restored, nil
}

// storeVerified streams src while deriving its SHA-256 and only commits it to
// the blob store when the digest matches the content-address hash, so corrupt
// bytes are never stored (R2-C4). Bounded memory: bytes land in a temp file,
// never in RAM. If the digest does not match, the temp file is discarded.
func (s *CASStore) storeVerified(hash string, src io.Reader) (ok bool) {
	tmpDir := filepath.Join(s.base, "rebuild")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		log.Printf("STORAGE REBUILD: cannot create rebuild temp dir: %v", err)
		return false
	}
	tmp, err := os.CreateTemp(tmpDir, "verified-*")
	if err != nil {
		log.Printf("STORAGE REBUILD: cannot create temp file for %s: %v", hash, err)
		return false
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), src); err != nil {
		return false
	}
	var sum [sha256.Size]byte
	if hex.EncodeToString(h.Sum(sum[:0])) != hash {
		return false
	}
	if err := tmp.Close(); err != nil {
		return false
	}
	f, err := os.Open(tmp.Name())
	if err != nil {
		return false
	}
	defer f.Close()
	if err := s.blobs.PutBlob(hash, f); err != nil {
		log.Printf("STORAGE REBUILD: failed to store verified blob %s: %v", hash, err)
		return false
	}
	return true
}

// quarantineMark records hash as quarantine-marked (mark-and-hold, R2-C2): the
// local bytes are treated as untrustworthy (reads degrade to ENOENT) but are
// NOT hard-deleted, so the self-heal loop can replace them from a verified
// survivor before final teardown. This is an option; the scrub's hard-delete
// quarantine (#924) remains the default when no verified survivor exists.
func (s *CASStore) quarantineMark(hash string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketQuarantine).Put([]byte(hash), []byte{1})
	})
}

// quarantineClear removes a quarantine mark after verified bytes land (R2-C2).
func (s *CASStore) quarantineClear(hash string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketQuarantine).Delete([]byte(hash))
	})
}

// quarantined reports whether hash is quarantine-marked.
func (s *CASStore) quarantined(hash string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var q bool
	_ = s.db.View(func(tx *bbolt.Tx) error {
		q = tx.Bucket(bucketQuarantine).Get([]byte(hash)) != nil
		return nil
	})
	return q
}

// quarantinedHashes lists every quarantine-marked hash (R2-C2).
func (s *CASStore) quarantinedHashes() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var hashes []string
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketQuarantine)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, _ []byte) error {
			hashes = append(hashes, string(k))
			return nil
		})
	})
	return hashes, err
}

// RepairCount returns the total number of blobs repaired by the self-heal loop
// since start (R2-C6: metrics count repairs). Consumers read this from the
// /metrics collector.
func (s *CASStore) RepairCount() uint64 {
	return s.repairs.Load()
}
