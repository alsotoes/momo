package metrics

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

// fakeElector flips leadership on demand.
type fakeElector struct{ leader atomic.Bool }

func (f *fakeElector) IsController() bool { return f.leader.Load() }

// recordingModeStore counts persisted modes.
type recordingModeStore struct {
	mu    sync.Mutex
	saved []int
}

func (r *recordingModeStore) LoadMode() (int, bool, error) { return 0, false, nil }

func (r *recordingModeStore) SaveMode(mode int) error {
	r.mu.Lock()
	r.saved = append(r.saved, mode)
	r.mu.Unlock()
	return nil
}

func (r *recordingModeStore) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.saved)
}

func electionCfg() common.Configuration {
	return common.Configuration{
		Global: common.ConfigurationGlobal{
			PolymorphicSystem: true,
			ReplicationOrder:  []int{1, 2, 3, 4},
			AuthToken:         "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6", // notsecret
		},
		Metrics: common.ConfigurationMetrics{Interval: 5, MinThreshold: 0.2, MaxThreshold: 0.8},
	}
}

func TestGetMetricsStandbyThenTakeover(t *testing.T) {
	defer goleak.VerifyNone(t)
	elector := &fakeElector{}
	SetControllerElector(elector)
	t.Cleanup(func() { SetControllerElector(nil) })

	store := &recordingModeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		GetMetrics(ctx, electionCfg(), 3, store)
		close(done)
	}()

	// Standby: no mode is announced while another node holds the lease.
	time.Sleep(60 * time.Millisecond)
	if store.count() != 0 {
		t.Fatalf("standby node must not persist/push a mode, got %v", store.saved)
	}

	// Takeover: the node announces the mode it resumed.
	elector.leader.Store(true)
	deadline := time.Now().Add(2 * time.Second)
	for store.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if store.count() == 0 {
		t.Fatal("new leader did not announce its resumed mode")
	}

	cancel()
	<-done
}

func TestGetMetricsStandbyStaysAliveUntilCancel(t *testing.T) {
	defer goleak.VerifyNone(t)
	SetControllerElector(&fakeElector{})
	t.Cleanup(func() { SetControllerElector(nil) })

	store := &recordingModeStore{}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	GetMetrics(ctx, electionCfg(), 1, store)
	if store.count() != 0 {
		t.Fatalf("standby node wrote a mode: %v", store.saved)
	}
}

func TestSetControllerElectorNilRestoresLegacy(t *testing.T) {
	SetControllerElector(nil)
	if !IsControllerNode(0) {
		t.Fatal("legacy rule: node 0 must control")
	}
	if IsControllerNode(1) {
		t.Fatal("legacy rule: node 1 must not control")
	}
	SetControllerElector(&fakeElector{})
	if IsControllerNode(0) || IsControllerNode(1) {
		t.Fatal("with an elector that holds no lease no node may control")
	}
	SetControllerElector(nil)
}
