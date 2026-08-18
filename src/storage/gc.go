package storage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"syscall"
	"time"

	"go.etcd.io/bbolt"
)

// GCConfig configures the garbage collector.
type GCConfig struct {
	// Interval is how often the GC sweeper runs.
	Interval time.Duration
	// TombstoneRetention is how long tombstones are kept before expiring.
	TombstoneRetention time.Duration
}

// DefaultGCConfig returns sensible defaults for the garbage collector.
func DefaultGCConfig() GCConfig {
	return GCConfig{
		Interval:           5 * time.Minute,
		TombstoneRetention: 24 * time.Hour,
	}
}

// StartGC launches the background garbage collector goroutine.
// It is safe to call at most once per CASStore instance.
func (s *CASStore) StartGC(cfg GCConfig) {
	s.gcWG.Add(1)
	go s.gcLoop(cfg)
}

func (s *CASStore) gcLoop(cfg GCConfig) {
	defer s.gcWG.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CAS GC: gcLoop panic recovered: %v", r)
		}
	}()

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.gcDone:
			return
		case <-ticker.C:
			if err := s.runGC(cfg); err != nil {
				log.Printf("CAS GC: sweep error: %v", err)
			}
		}
	}
}

func (s *CASStore) runGC(cfg GCConfig) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CAS GC: runGC panic recovered: %v", r)
			err = fmt.Errorf("CAS GC panic: %w", syscall.EIO)
		}
	}()
	if err := s.sweepOrphanedBlobs(); err != nil {
		return err
	}
	return s.sweepExpiredTombstones(cfg.TombstoneRetention)
}

// sweepOrphanedBlobs removes blob files and objects entries with RefCount=0.
// Blob deletions (which may involve network I/O for S3 backends) are performed
// OUTSIDE the bbolt write transaction to avoid blocking all db operations.
func (s *CASStore) sweepOrphanedBlobs() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var orphanedHashes []string

	err := s.db.Update(func(tx *bbolt.Tx) error {
		obj := tx.Bucket(bucketObjects)
		c := obj.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if len(v) != 24 {
				continue
			}
			meta, err := decodeObjectMeta(v)
			if err != nil {
				return fmt.Errorf("CAS GC: failed to decode metadata for blob %s: %w", k, err)
			}
			if meta.RefCount <= 0 {
				hash := string(k)
				orphanedHashes = append(orphanedHashes, hash)
				if err := obj.Delete([]byte(hash)); err != nil {
					log.Printf("CAS GC: failed to delete metadata for blob %s: %v", hash, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, hash := range orphanedHashes {
		if err := s.blobs.DeleteBlob(hash); err != nil {
			log.Printf("CAS GC: failed to remove blob %s: %v", hash, err)
		}
	}

	if len(orphanedHashes) > 0 {
		log.Printf("CAS GC: removed %d orphaned blob(s)", len(orphanedHashes))
	}
	return nil
}

// sweepExpiredTombstones removes tombstones older than the retention period.
func (s *CASStore) sweepExpiredTombstones(retention time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-retention).UnixNano()
	var expiredNames [][]byte

	err := s.db.Update(func(tx *bbolt.Tx) error {
		ts := tx.Bucket(bucketTombstones)
		c := ts.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if len(v) < 8 {
				expiredNames = append(expiredNames, bytes.Clone(k))
				continue
			}
			deletedAt := int64(binary.BigEndian.Uint64(v[:8]))
			if deletedAt < cutoff {
				expiredNames = append(expiredNames, bytes.Clone(k))
			}
		}

		for _, name := range expiredNames {
			// 🔊 Log Delete failures instead of swallowing them; sweep
			// continues so one bad entry can't block the rest.
			if err := ts.Delete(name); err != nil {
				log.Printf("CAS GC: failed to delete expired tombstone for %q: %v", name, err)
			}
		}
		return nil
	})

	if len(expiredNames) > 0 {
		log.Printf("CAS GC: expired %d tombstone(s)", len(expiredNames))
	}
	return err
}

// GetTombstones returns all active tombstones (name -> deletion timestamp).
// This enables P2P nodes to exchange delete information for eventual consistency.
func (s *CASStore) GetTombstones() (map[string]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tombstones := make(map[string]int64)
	err := s.db.View(func(tx *bbolt.Tx) error {
		ts := tx.Bucket(bucketTombstones)
		if ts == nil {
			return nil
		}
		c := ts.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if len(v) >= 8 {
				tombstones[string(k)] = int64(binary.BigEndian.Uint64(v[:8]))
			}
		}
		return nil
	})
	return tombstones, err
}

// ApplyTombstone records a tombstone for a name that was deleted on a remote peer.
// This is used during P2P tombstone exchange to propagate deletes.
func (s *CASStore) ApplyTombstone(name string, deletedAt int64) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Recovered from panic in ApplyTombstone: %v", r)
			err = fmt.Errorf("panic in ApplyTombstone: %v: %w", r, syscall.EIO)
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()

	var orphanedHash string
	err = s.db.Update(func(tx *bbolt.Tx) error {
		ts := tx.Bucket(bucketTombstones)
		existing := ts.Get([]byte(name))
		if existing != nil {
			if len(existing) >= 8 {
				existingTs := int64(binary.BigEndian.Uint64(existing[:8]))
				if existingTs >= deletedAt {
					return nil
				}
			}
		}

		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(deletedAt))
		if err := ts.Put([]byte(name), buf[:]); err != nil {
			return err
		}

		ns := tx.Bucket(bucketNamespace)
		paths := tx.Bucket(bucketPaths)
		obj := tx.Bucket(bucketObjects)

		h := ns.Get([]byte(name))
		if h != nil {
			hash := string(h)
			if val := obj.Get([]byte(hash)); val != nil {
				decoded, err := decodeObjectMeta(val)
				if err != nil {
					return fmt.Errorf("failed to decode metadata for hash %s: %w", hash, err)
				}
				meta := decoded
				meta.RefCount--
				if meta.RefCount <= 0 {
					meta.RefCount = 0
					meta.DeletedAt = deletedAt
					// 🛡️ CVE-006: Mirror Delete's immediate-deletion intent. When
					// refcount reaches 0 the blob is orphaned; record it so we can
					// delete its content right after the transaction commits.
					orphanedHash = hash
				}
				if err := obj.Put([]byte(hash), meta.encode()); err != nil {
					return fmt.Errorf("metadata update error: %w", syscall.EIO)
				}
			}
		}
		if err := ns.Delete([]byte(name)); err != nil {
			return fmt.Errorf("namespace delete error: %w", err)
		}
		if err := paths.Delete([]byte(name)); err != nil {
			return fmt.Errorf("paths delete error: %w", err)
		}
		if err := tx.Bucket(bucketModTimes).Delete([]byte(name)); err != nil {
			return fmt.Errorf("modtime delete error: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 🛡️ CVE-006: Immediately delete blob content when refcount reaches 0, same
	// as CASStore.Delete. Performed outside the bbolt transaction to avoid
	// blocking all db operations during potential network I/O (S3 backends).
	if orphanedHash != "" {
		if delErr := s.blobs.DeleteBlob(orphanedHash); delErr != nil {
			log.Printf("AUDIT: Failed to delete orphaned blob %s: %v", orphanedHash, delErr)
		} else if metaErr := s.db.Update(func(tx *bbolt.Tx) error {
			return tx.Bucket(bucketObjects).Delete([]byte(orphanedHash))
		}); metaErr != nil {
			log.Printf("AUDIT: Failed to remove metadata for orphaned blob %s: %v", orphanedHash, metaErr)
		}
	}
	return nil
}
