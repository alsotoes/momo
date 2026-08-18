package server

import (
	"fmt"
	"log"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/p2p"
	"github.com/alsotoes/momo/src/storage"
)

// ScatterGatherLister adapts p2p.ScatterGather to the transport.GlobalLister interface.
type ScatterGatherLister struct {
	sg      *p2p.ScatterGather
	store   storage.Store
	timeout time.Duration
}

// NewScatterGatherLister creates a new ScatterGatherLister adapter.
func NewScatterGatherLister(sg *p2p.ScatterGather, store storage.Store, timeout time.Duration) *ScatterGatherLister {
	return &ScatterGatherLister{sg: sg, store: store, timeout: timeout}
}

// GlobalList queries all peers for their local file lists, includes the local node's files, and merges and deduplicates results.
func (s *ScatterGatherLister) GlobalList(timeout time.Duration) ([]common.FileMetadata, error) {
	if s.sg == nil {
		return nil, fmt.Errorf("scatter-gather not initialized")
	}

	var allLists [][]common.FileMetadata

	if s.store != nil {
		localFiles, err := s.store.List()
		if err != nil {
			log.Printf("AUDIT: GlobalList local store error: %v", common.SanitizeLog(err.Error()))
		} else if len(localFiles) > 0 {
			allLists = append(allLists, localFiles)
		}
	}

	responses, count := s.sg.Query(p2p.QueryList, nil, timeout)
	if count == 0 && len(allLists) == 0 {
		return nil, nil
	}

	for _, resp := range responses {
		if resp.Error != "" {
			log.Printf("AUDIT: GlobalList peer error: %s", common.SanitizeLog(resp.Error))
			continue
		}
		files, err := DecodeFileMetadataList(resp.Data)
		if err != nil {
			log.Printf("AUDIT: GlobalList decode failed from peer: %v", common.SanitizeLog(err.Error()))
			continue
		}
		allLists = append(allLists, files)
	}

	return MergeFileMetadataLists(allLists...), nil
}

// leaseManager is the minimal contract LeaseAcquirerAdapter needs. It is
// satisfied by *p2p.LeaseManager and lets tests inject a recording fake.
type leaseManager interface {
	Acquire(key string, duration time.Duration) (*p2p.Lease, error)
	ReleaseByKey(key string) error
}

// LeaseAcquirerAdapter adapts p2p.LeaseManager to the transport.LeaseAcquirer interface.
type LeaseAcquirerAdapter struct {
	lm leaseManager
}

// NewLeaseAcquirerAdapter creates a new LeaseAcquirerAdapter.
func NewLeaseAcquirerAdapter(lm *p2p.LeaseManager) *LeaseAcquirerAdapter {
	return &LeaseAcquirerAdapter{lm: lm}
}

// AcquireLease acquires a lease for the given key for the caller-provided
// timeout duration (fix #666). The caller's timeout must be respected rather
// than a value captured at construction.
func (l *LeaseAcquirerAdapter) AcquireLease(key string, timeout time.Duration) error {
	if l.lm == nil {
		return fmt.Errorf("lease manager not initialized")
	}
	_, err := l.lm.Acquire(key, timeout)
	return err
}

// ReleaseLease releases the lease for the given key.
func (l *LeaseAcquirerAdapter) ReleaseLease(key string) error {
	if l.lm == nil {
		return nil
	}
	return l.lm.ReleaseByKey(key)
}

// ScatterGatherDeleter adapts p2p.ScatterGather to the transport.DeletePropagator interface.
type ScatterGatherDeleter struct {
	sg      *p2p.ScatterGather
	timeout time.Duration
}

// NewScatterGatherDeleter creates a new ScatterGatherDeleter adapter.
func NewScatterGatherDeleter(sg *p2p.ScatterGather, timeout time.Duration) *ScatterGatherDeleter {
	return &ScatterGatherDeleter{sg: sg, timeout: timeout}
}

// PropagateDelete fans out a delete operation to all peers via scatter-gather.
// It returns an error whenever ANY contacted peer fails, so a partial
// propagation (stale replicas still holding the deleted object) is surfaced to
// the caller rather than being masked by a single success (issue #633).
func (d *ScatterGatherDeleter) PropagateDelete(key string, timeout time.Duration) error {
	if d.sg == nil {
		return nil
	}
	results, count := d.sg.Query(p2p.QueryDelete, []byte(key), timeout)
	if count == 0 {
		return fmt.Errorf("propagate delete: no peers responded (errno=%d)", syscall.EHOSTUNREACH)
	}

	successes := 0
	var firstErr string
	for _, resp := range results {
		if resp.Error == "" {
			successes++
			continue
		}
		if firstErr == "" {
			firstErr = resp.Error
		}
	}
	failures := count - successes
	if failures > 0 {
		return fmt.Errorf("propagate delete: %d/%d peers failed to delete (error: %s) (errno=%d)",
			failures, count, firstErr, syscall.EIO)
	}
	return nil
}
