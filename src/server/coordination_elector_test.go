package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/metrics"
	"github.com/alsotoes/momo/src/p2p"
	"go.uber.org/goleak"
)

// fakeGranter simulates lease acquisition, renewal and failure.
type fakeGranter struct {
	mu         sync.Mutex
	acquireErr error
	renewErr   error
	acquires   int
	renews     int
	releases   int
	lease      *p2p.Lease
}

func (f *fakeGranter) Acquire(string, time.Duration) (*p2p.Lease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquires++
	if f.acquireErr != nil {
		return nil, f.acquireErr
	}
	f.lease = &p2p.Lease{LeaseID: 7, Key: metricsControllerLeaseKey, Expiry: time.Now().Add(time.Minute), QuorumSize: 2}
	return f.lease, nil
}

func (f *fakeGranter) Renew(lease *p2p.Lease, d time.Duration) (*p2p.Lease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renews++
	if f.renewErr != nil {
		return nil, f.renewErr
	}
	renewed := *lease
	renewed.Expiry = time.Now().Add(d)
	f.lease = &renewed
	return &renewed, nil
}

func (f *fakeGranter) Release(*p2p.Lease) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	return nil
}

func (f *fakeGranter) setRenewErr(err error) {
	f.mu.Lock()
	f.renewErr = err
	f.mu.Unlock()
}

func (f *fakeGranter) counts() (acquires, renews, releases int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acquires, f.renews, f.releases
}

func TestMetricsControllerElectorAcquiresAndRenews(t *testing.T) {
	f := &fakeGranter{}
	e := newMetricsControllerElector(f, 300*time.Millisecond)
	if e.IsController() {
		t.Fatal("must not be controller before the first tick")
	}
	e.tick()
	if !e.IsController() {
		t.Fatal("expected leadership after a successful acquire")
	}
	e.tick() // renew, not a second acquire
	acquires, renews, _ := f.counts()
	if acquires != 1 || renews != 1 {
		t.Fatalf("acquires=%d renews=%d, want 1 and 1", acquires, renews)
	}
}

func TestMetricsControllerElectorStepsDownOnRenewFailure(t *testing.T) {
	f := &fakeGranter{}
	e := newMetricsControllerElector(f, time.Minute)
	e.tick()
	if !e.IsController() {
		t.Fatal("expected leadership")
	}
	f.setRenewErr(errors.New("quorum lost"))
	e.tick()
	if e.IsController() {
		t.Fatal("must step down when renewal fails")
	}
	// A later successful campaign restores leadership.
	f.setRenewErr(nil)
	e.tick()
	if !e.IsController() {
		t.Fatal("expected re-acquisition after recovery")
	}
}

func TestMetricsControllerElectorStaysDownWhileAcquireFails(t *testing.T) {
	f := &fakeGranter{acquireErr: errors.New("no quorum")}
	e := newMetricsControllerElector(f, time.Minute)
	e.tick()
	if e.IsController() {
		t.Fatal("must not lead without a lease")
	}
}

func TestMetricsControllerElectorRunReleasesOnStop(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := &fakeGranter{}
	e := newMetricsControllerElector(f, 300*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !e.IsController() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !e.IsController() {
		t.Fatal("elector never acquired the lease")
	}
	cancel()
	<-done
	_, _, releases := f.counts()
	if releases != 1 {
		t.Fatalf("releases=%d, want 1 (graceful handoff)", releases)
	}
	if e.IsController() {
		t.Fatal("must not claim leadership after shutdown")
	}
}

func TestElectionGateControlsAnyNode(t *testing.T) {
	defer goleak.VerifyNone(t)
	metrics.SetControllerElector(nil)
	t.Cleanup(func() { metrics.SetControllerElector(nil) })

	f := &fakeGranter{}
	e := newMetricsControllerElector(f, time.Minute)
	metrics.SetControllerElector(e)
	if metrics.IsControllerNode(2) {
		t.Fatal("node 2 must not control before acquiring the lease")
	}
	e.tick()
	if !metrics.IsControllerNode(2) {
		t.Fatal("node 2 must control while holding the lease (#1144)")
	}
	// Removing the elector restores the legacy node-0 rule.
	metrics.SetControllerElector(nil)
	if !metrics.IsControllerNode(0) || metrics.IsControllerNode(2) {
		t.Fatal("legacy rule must be node 0 only")
	}
}
