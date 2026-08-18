package server

import (
	"sync"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/p2p"
)

// recordingLeaseManager satisfies leaseManager and records the durations
// passed to Acquire so tests can assert the caller's timeout is honored.
type recordingLeaseManager struct {
	mu        sync.Mutex
	acquired  []string
	durations []time.Duration
}

func (r *recordingLeaseManager) Acquire(key string, duration time.Duration) (*p2p.Lease, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.acquired = append(r.acquired, key)
	r.durations = append(r.durations, duration)
	return &p2p.Lease{Key: key, Expiry: time.Now().Add(duration)}, nil
}

func (r *recordingLeaseManager) ReleaseByKey(key string) error {
	return nil
}

// TestAcquireLeaseHonorsCallerTimeout verifies the caller-provided timeout is
// passed through to the lease manager rather than being replaced by a value
// captured at construction (fix #666).
func TestAcquireLeaseHonorsCallerTimeout(t *testing.T) {
	rl := &recordingLeaseManager{}
	adapter := &LeaseAcquirerAdapter{lm: rl}

	if err := adapter.AcquireLease("key-a", 37*time.Millisecond); err != nil {
		t.Fatalf("AcquireLease failed: %v", err)
	}
	if err := adapter.AcquireLease("key-b", 5*time.Second); err != nil {
		t.Fatalf("AcquireLease failed: %v", err)
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if len(rl.durations) != 2 {
		t.Fatalf("expected 2 Acquire calls, got %d", len(rl.durations))
	}
	if rl.durations[0] != 37*time.Millisecond {
		t.Errorf("first Acquire duration = %v, want 37ms (caller timeout)", rl.durations[0])
	}
	if rl.durations[1] != 5*time.Second {
		t.Errorf("second Acquire duration = %v, want 5s (caller timeout)", rl.durations[1])
	}
}

// TestAcquireLeaseNilManager verifies the nil-guard is intact.
func TestAcquireLeaseNilManager(t *testing.T) {
	adapter := &LeaseAcquirerAdapter{lm: nil}
	if err := adapter.AcquireLease("key", time.Second); err == nil {
		t.Fatal("expected error when lease manager is nil")
	}
}
