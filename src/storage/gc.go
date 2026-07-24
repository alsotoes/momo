package storage

import (
	"encoding/binary"
	"log"
	"os"
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

func (s *CASStore) runGC(cfg GCConfig) error {
	if err := s.sweepOrphanedBlobs(); err != nil {
		return err
	}
	return s.sweepExpiredTombstones(cfg.TombstoneRetention)
}

// sweepOrphanedBlobs removes blob files and objects entries with RefCount=0.
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
			meta := decodeObjectMeta(v)
			if meta.RefCount <= 0 {
				hash := string(k)
				blobPath := s.getBlobPath(hash)
				if err := os.Remove(blobPath); err != nil && !os.IsNotExist(err) {
					log.Printf("CAS GC: failed to remove blob %s: %v", blobPath, err)
					continue
				}
				orphanedHashes = append(orphanedHashes, hash)
			}
		}

		for _, hash := range orphanedHashes {
			obj.Delete([]byte(hash))
		}
		return nil
	})

	if len(orphanedHashes) > 0 {
		log.Printf("CAS GC: removed %d orphaned blob(s)", len(orphanedHashes))
	}
	return err
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
				expiredNames = append(expiredNames, k)
				continue
			}
			deletedAt := int64(binary.BigEndian.Uint64(v[:8]))
			if deletedAt < cutoff {
				expiredNames = append(expiredNames, k)
			}
		}

		for _, name := range expiredNames {
			ts.Delete(name)
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
func (s *CASStore) ApplyTombstone(name string, deletedAt int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bbolt.Tx) error {
		ts := tx.Bucket(bucketTombstones)
		existing := ts.Get([]byte(name))
		if existing != nil {
			existingTs := int64(binary.BigEndian.Uint64(existing[:8]))
			if existingTs >= deletedAt {
				return nil
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
				meta := decodeObjectMeta(val)
				meta.RefCount--
				if meta.RefCount <= 0 {
					meta.RefCount = 0
					meta.DeletedAt = deletedAt
				}
				obj.Put([]byte(hash), meta.encode())
			}
		}
		ns.Delete([]byte(name))
		paths.Delete([]byte(name))
		return nil
	})
}
