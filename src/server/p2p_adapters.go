package server

import (
	"fmt"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/p2p"
)

// ScatterGatherLister adapts p2p.ScatterGather to the transport.GlobalLister interface.
type ScatterGatherLister struct {
	sg      *p2p.ScatterGather
	timeout time.Duration
}

// NewScatterGatherLister creates a new ScatterGatherLister adapter.
func NewScatterGatherLister(sg *p2p.ScatterGather, timeout time.Duration) *ScatterGatherLister {
	return &ScatterGatherLister{sg: sg, timeout: timeout}
}

// GlobalList queries all peers for their local file lists and: merges and deduplicates results.
func (s *ScatterGatherLister) GlobalList(timeout time.Duration) ([]common.FileMetadata, error) {
	if s.sg == nil {
		return nil, fmt.Errorf("scatter-gather not initialized")
	}

	responses, count := s.sg.Query(p2p.QueryList, nil, timeout)
	if count == 0 {
		return nil, nil
	}

	var allLists [][]common.FileMetadata
	for _, resp := range responses {
		if resp.Error != "" {
			continue
		}
		files, err := DecodeFileMetadataList(resp.Data)
		if err != nil {
			continue
		}
		allLists = append(allLists, files)
	}

	return MergeFileMetadataLists(allLists...), nil
}

// LeaseAcquirerAdapter adapts p2p.LeaseManager to the transport.LeaseAcquirer interface.
type LeaseAcquirerAdapter struct {
	lm       *p2p.LeaseManager
	duration time.Duration
}

// NewLeaseAcquirerAdapter creates a new LeaseAcquirerAdapter.
func NewLeaseAcquirerAdapter(lm *p2p.LeaseManager, duration time.Duration) *LeaseAcquirerAdapter {
	return &LeaseAcquirerAdapter{lm: lm, duration: duration}
}

// AcquireLease acquires a lease for the given key.
func (l *LeaseAcquirerAdapter) AcquireLease(key string, timeout time.Duration) error {
	if l.lm == nil {
		return fmt.Errorf("lease manager not initialized")
	}
	_, err := l.lm.Acquire(key, l.duration)
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
func (d *ScatterGatherDeleter) PropagateDelete(key string, timeout time.Duration) error {
	if d.sg == nil {
		return nil
	}
	d.sg.Query(p2p.QueryDelete, []byte(key), timeout)
	return nil
}
