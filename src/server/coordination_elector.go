package server

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alsotoes/momo/src/metrics"
	"github.com/alsotoes/momo/src/p2p"
)

// metricsControllerLeaseKey is the lease key the metrics-controller role
// campaigns for. Only the holder runs the polymorphic replication-mode loop
// (#1144), which removes the node-0 single point of failure.
const metricsControllerLeaseKey = "coordination/metrics-controller"

// leaseGranter is the subset of p2p.LeaseManager the elector needs, so the
// election logic is testable without a transport.
type leaseGranter interface {
	Acquire(key string, duration time.Duration) (*p2p.Lease, error)
	Renew(lease *p2p.Lease, duration time.Duration) (*p2p.Lease, error)
	Release(lease *p2p.Lease) error
}

// metricsControllerElector campaigns for and renews the metrics-controller
// lease. Losing the lease stops the node from acting, without stopping the
// metrics loop: it keeps standing by and can take over again.
type metricsControllerElector struct {
	granter leaseGranter
	ttl     time.Duration

	mu     sync.Mutex
	lease  *p2p.Lease
	active atomic.Bool
}

// newMetricsControllerElector builds an elector with a sane TTL.
func newMetricsControllerElector(granter leaseGranter, ttl time.Duration) *metricsControllerElector {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &metricsControllerElector{granter: granter, ttl: ttl}
}

// IsController reports whether this node currently holds the lease.
func (e *metricsControllerElector) IsController() bool {
	return e != nil && e.active.Load()
}

// Run campaigns and renews until ctx is canceled. It renews at a third of the
// TTL, so two consecutive renewals can fail before a peer may take over.
func (e *metricsControllerElector) Run(ctx context.Context) {
	if e == nil || e.granter == nil {
		return
	}
	interval := e.ttl / 3
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	e.tick()
	for {
		select {
		case <-ctx.Done():
			e.resign()
			return
		case <-ticker.C:
			e.tick()
		}
	}
}

// tick performs one campaign-or-renew step.
func (e *metricsControllerElector) tick() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lease == nil {
		lease, err := e.granter.Acquire(metricsControllerLeaseKey, e.ttl)
		if err != nil {
			if e.active.Load() {
				e.active.Store(false)
				log.Printf("AUDIT: lost metrics-controller role (acquire failed): %v", err)
			}
			return
		}
		e.lease = lease
		if !e.active.Swap(true) {
			log.Printf("Coordination: became metrics controller (lease %d, quorum %d)", lease.LeaseID, lease.QuorumSize)
		}
		return
	}
	renewed, err := e.granter.Renew(e.lease, e.ttl)
	if err != nil {
		log.Printf("AUDIT: metrics-controller renewal failed: %v", err)
		e.lease = nil
		if e.active.Swap(false) {
			log.Printf("Coordination: stepped down from the metrics-controller role")
		}
		return
	}
	e.lease = renewed
}

// resign releases the lease on shutdown so a peer can take over immediately
// instead of waiting for expiry.
func (e *metricsControllerElector) resign() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lease != nil && e.granter != nil {
		if err := e.granter.Release(e.lease); err != nil {
			log.Printf("AUDIT: metrics-controller release failed: %v", err)
		}
	}
	e.lease = nil
	e.active.Store(false)
}

// installMetricsControllerElection wires the elector into the metrics loop and
// starts its campaign goroutine. Called from the P2P bootstrap when
// [coordination] enabled=true.
func installMetricsControllerElection(ctx context.Context, ttlSeconds int, lm *p2p.LeaseManager) {
	ttl := time.Duration(ttlSeconds) * time.Second
	elector := newMetricsControllerElector(lm, ttl)
	metrics.SetControllerElector(elector)
	go elector.Run(ctx)
	log.Printf("Coordination: metrics-controller election enabled (lease_ttl=%s)", ttl)
}
