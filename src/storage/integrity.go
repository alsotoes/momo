package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"go.etcd.io/bbolt"
)

// contentHashVerifiedError marks blob content that no longer matches its
// content-address key. Reads surface it so corrupt bytes are never served.
func contentHashVerifiedError(key string) error {
	return fmt.Errorf("%w: blob %s no longer matches its content address: %w", common.ErrIntegrityMismatch, key, syscall.EBADMSG)
}

// verifyingReader recomputes the SHA-256 of a stream and, at EOF, asserts it
// equals the expected content-address key. On mismatch it returns an integrity
// error instead of a clean io.EOF, so corrupt bytes are never reported as a
// successful read. It is bounded-memory: it streams through the caller's
// buffer and only holds a SHA-256 state.
type verifyingReader struct {
	src        io.Reader
	h          sha256Alg
	expected   string
	checked    bool
	corrupt    error
	onVerified func() // optional callback fired once a full match is confirmed
	onCorrupt  func() // optional callback fired when a mismatch is confirmed
}

// sha256Alg avoids dragging the concrete hash.Hash name into the struct.
type sha256Alg = interface {
	Write(p []byte) (int, error)
	Sum(b []byte) []byte
}

func newVerifyingReader(src io.Reader, expected string) *verifyingReader {
	return &verifyingReader{src: src, h: sha256.New(), expected: expected}
}

func (v *verifyingReader) Read(p []byte) (int, error) {
	if v.corrupt != nil {
		return 0, v.corrupt
	}
	n, err := v.src.Read(p)
	if n > 0 {
		v.h.Write(p[:n])
	}
	if err == io.EOF && !v.checked {
		v.checked = true
		var sum [sha256.Size]byte
		digest := v.h.Sum(sum[:0])
		key := hex.EncodeToString(digest)
		if key != v.expected {
			v.corrupt = contentHashVerifiedError(v.expected)
			if v.onCorrupt != nil {
				v.onCorrupt()
			}
			return n, v.corrupt
		}
		if v.onVerified != nil {
			v.onVerified()
		}
	}
	return n, err
}

// verifyingReadCloser wraps a verifyingReader over an underlying ReadCloser and
// forwards Close to the underlying stream.
type verifyingReadCloser struct {
	*verifyingReader
	underlying io.Closer
}

func (v *verifyingReadCloser) Close() error { return v.underlying.Close() }

// ScrubConfig configures the background integrity scrub.
type ScrubConfig struct {
	// Interval is how often the scrub passes over referenced blobs.
	Interval time.Duration
}

// DefaultScrubConfig returns sensible defaults for the scrubber (1 hour).
func DefaultScrubConfig() ScrubConfig {
	return ScrubConfig{Interval: time.Hour}
}

// StartScrub launches the background integrity scrub goroutine. It is safe to
// call at most once per CASStore instance — the sync.Once guard makes repeated
// invocations no-ops (mirroring StartGC) so multiple scrub loops cannot spawn.
func (s *CASStore) StartScrub(cfg ScrubConfig) {
	s.scrubOnce.Do(func() {
		s.scrubWG.Add(1)
		go s.scrubLoop(cfg)
	})
}

func (s *CASStore) scrubLoop(cfg ScrubConfig) {
	s.scrubStarted.Store(1)
	defer s.scrubWG.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("STORAGE SCRUB: scrubLoop panic recovered: %v", r)
		}
	}()

	interval := cfg.Interval
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.scrubDone:
			return
		case <-ticker.C:
			if err := s.runScrub(); err != nil {
				log.Printf("STORAGE SCRUB: pass error: %v", err)
			}
		}
	}
}

// runScrub performs a single integrity pass over all referenced blobs. Blob
// reads (which may be network I/O for S3 backends) happen outside s.mu to avoid
// blocking concurrent store operations.
func (s *CASStore) runScrub() (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("STORAGE SCRUB: runScrub panic recovered: %v", r)
			err = fmt.Errorf("storage scrub panic: %w", syscall.EIO)
		}
	}()

	hashes, err := s.referencedBlobs()
	if err != nil {
		return err
	}

	var quarantined []string
	for _, hash := range hashes {
		canceled, ok, qerr := s.scrubBlob(hash)
		if qerr != nil {
			log.Printf("STORAGE SCRUB: failed to verify blob %s: %v", hash, qerr)
			continue
		}
		if canceled {
			return nil
		}
		if ok {
			quarantined = append(quarantined, hash)
		}
	}

	if len(quarantined) > 0 {
		log.Printf("AUDIT: STORAGE SCRUB quarantined %d corrupted blob(s): %v", len(quarantined), quarantined)
	} else {
		log.Printf("STORAGE SCRUB: pass complete, %d blob(s) verified, 0 corrupted", len(hashes))
	}
	return nil
}

// referencedBlobs returns the content-hash keys of all referenced (non-orphaned)
// blobs, under a short bbolt read transaction.
func (s *CASStore) referencedBlobs() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var hashes []string
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketObjects)
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if len(v) != 24 {
				continue
			}
			meta, err := decodeObjectMeta(v)
			if err != nil {
				return fmt.Errorf("failed to decode metadata for blob %s: %w", k, err)
			}
			if meta.RefCount > 0 {
				hashes = append(hashes, string(k))
			}
		}
		return nil
	})
	return hashes, err
}

// scrubBlob re-reads and re-hashes a single blob. It reports whether the blob
// was quarantined and whether the store is being closed (canceled).
func (s *CASStore) scrubBlob(hash string) (canceled, quarantined bool, err error) {
	select {
	case <-s.scrubDone:
		return true, false, nil
	default:
	}

	rc, err := s.blobs.GetBlob(hash)
	if err != nil {
		if err == syscall.ENOENT {
			return false, false, nil
		}
		return false, false, err
	}
	digest, rerr := common.HashReader(rc)
	closeErr := rc.Close()
	if rerr != nil {
		if closeErr != nil {
			log.Printf("STORAGE SCRUB: close after hash error for %s: %v", hash, closeErr)
		}
		return false, false, rerr
	}
	if closeErr != nil {
		return false, false, closeErr
	}
	if digest != hash {
		if err := s.quarantine(hash); err != nil {
			return false, false, err
		}
		return false, true, nil
	}
	// A full re-read re-hashed the blob to match its content-address key: mark
	// it trusted so verifiedCache skips redundant hashing on later reads (Win1,
	// #950). CAS immutability makes this trust permanent; next scrub re-catches rot.
	s.verifier.MarkTrusted(hash)
	return false, false, nil
}

// quarantine removes a corrupted blob's content and its object metadata so
// later reads for names mapping to that hash fail explicitly with ENOENT. The
// blob deletion (possibly network I/O for S3 backends) runs outside the write
// transaction, mirroring the orphan-deletion pattern.
func (s *CASStore) quarantine(hash string) error {
	if delErr := s.blobs.DeleteBlob(hash); delErr != nil {
		log.Printf("AUDIT: STORAGE SCRUB failed to delete corrupted blob %s: %v", hash, delErr)
		return delErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if metaErr := s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketObjects).Delete([]byte(hash))
	}); metaErr != nil {
		log.Printf("AUDIT: STORAGE SCRUB failed to remove metadata for corrupted blob %s: %v", hash, metaErr)
		return metaErr
	}
	return nil
}
