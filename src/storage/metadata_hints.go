package storage

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"

	bolt "go.etcd.io/bbolt"
)

// MetadataHint is a metadata record owed to one peer that could not be reached
// during a quorum write (hinted handoff, R6 v2). Payload is the encoded RPC
// argument, opaque to the store.
type MetadataHint struct {
	Peer      int32
	Name      string
	Payload   []byte
	CreatedAt time.Time
}

// hintKey composes the bucket key: big-endian peer id followed by the name.
func hintKey(peer int32, name string) []byte {
	k := make([]byte, 4+len(name))
	binary.BigEndian.PutUint32(k[:4], uint32(peer))
	copy(k[4:], name)
	return k
}

// PutMetadataHint stores one pending record for a peer, replacing any earlier
// hint for the same name (last write wins, which is what a replay should send).
func (s *CASStore) PutMetadataHint(peer int32, name string, payload []byte, now time.Time) (err error) {
	if name == "" || len(payload) == 0 {
		return fmt.Errorf("metadata hint needs a name and payload: %w", syscall.EINVAL)
	}
	value := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint64(value[:8], uint64(now.UnixNano()))
	copy(value[8:], payload)
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMetadataHints)
		if b == nil {
			return fmt.Errorf("metadata hints bucket missing: %w", syscall.EIO)
		}
		return b.Put(hintKey(peer, name), value)
	})
}

// ListMetadataHints returns every hint owed to a peer, oldest first.
func (s *CASStore) ListMetadataHints(peer int32) (hints []MetadataHint, err error) {
	err = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMetadataHints)
		if b == nil {
			return nil
		}
		prefix := hintKey(peer, "")
		c := b.Cursor()
		for k, v := c.Seek(prefix); len(k) >= 4 && binary.BigEndian.Uint32(k[:4]) == uint32(peer); k, v = c.Next() {
			if len(v) < 8 {
				continue
			}
			payload := make([]byte, len(v)-8)
			copy(payload, v[8:])
			hints = append(hints, MetadataHint{
				Peer:      peer,
				Name:      string(k[4:]),
				Payload:   payload,
				CreatedAt: time.Unix(0, int64(binary.BigEndian.Uint64(v[:8]))),
			})
		}
		return nil
	})
	return hints, err
}

// DeleteMetadataHint removes one hint after a successful replay.
func (s *CASStore) DeleteMetadataHint(peer int32, name string) (err error) {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMetadataHints)
		if b == nil {
			return nil
		}
		return b.Delete(hintKey(peer, name))
	})
}

// PruneMetadataHints drops hints older than before and reports how many went
// away. Called before storing new hints so a long outage cannot grow the bucket
// without bound.
func (s *CASStore) PruneMetadataHints(before time.Time) (pruned int, err error) {
	cutoff := uint64(before.UnixNano())
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMetadataHints)
		if b == nil {
			return nil
		}
		var stale [][]byte
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if len(v) >= 8 && binary.BigEndian.Uint64(v[:8]) < cutoff {
				stale = append(stale, append([]byte(nil), k...))
			}
		}
		for _, k := range stale {
			if derr := b.Delete(k); derr != nil {
				return derr
			}
			pruned++
		}
		return nil
	})
	return pruned, err
}
