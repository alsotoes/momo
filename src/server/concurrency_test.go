package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestAdaptiveConcurrency_ProbeBounds(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctrl := NewAdaptiveConcurrencyController()
	capLimit := ctrl.Capacity()

	if capLimit < MinConcurrentConnections || capLimit > MaxConcurrentConnections {
		t.Fatalf("expected capacity within [%d, %d], got %d", MinConcurrentConnections, MaxConcurrentConnections, capLimit)
	}

	if ctrl.CurrentLimit() != capLimit {
		t.Fatalf("expected initial CurrentLimit %d to match capacity %d", ctrl.CurrentLimit(), capLimit)
	}
}

func TestAdaptiveConcurrency_AcquireAndRelease(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctrl := NewAdaptiveConcurrencyControllerWithCapacity(3)

	ctx := context.Background()

	// Acquire 3 slots
	if !ctrl.AcquireSlot(ctx) || !ctrl.AcquireSlot(ctx) || !ctrl.AcquireSlot(ctx) {
		t.Fatal("failed to acquire available slots")
	}

	if ctrl.Active() != 3 {
		t.Fatalf("expected 3 active slots, got %d", ctrl.Active())
	}

	// 4th acquire should fail with homeostatic check or timeout
	ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	if ctrl.AcquireSlot(ctxTimeout) {
		t.Fatal("expected slot acquisition to fail when capacity is full")
	}

	// Release 1 slot
	ctrl.ReleaseSlot()
	if ctrl.Active() != 2 {
		t.Fatalf("expected 2 active slots, got %d", ctrl.Active())
	}

	// Now acquisition should succeed
	if !ctrl.AcquireSlot(ctx) {
		t.Fatal("expected slot acquisition to succeed after release")
	}
}

func TestAdaptiveConcurrency_ContextCancellation(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctrl := NewAdaptiveConcurrencyControllerWithCapacity(1)

	ctx := context.Background()
	if !ctrl.AcquireSlot(ctx) {
		t.Fatal("failed to acquire initial slot")
	}

	ctxCanceled, cancel := context.WithCancel(ctx)
	cancel() // pre-cancel context

	done := make(chan bool)
	go func() {
		done <- ctrl.AcquireSlot(ctxCanceled)
	}()

	select {
	case ok := <-done:
		if ok {
			t.Fatal("expected false on canceled context")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("AcquireSlot blocked indefinitely on canceled context")
	}
}

func TestAdaptiveConcurrency_HomeostaticThrottling(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctrl := NewAdaptiveConcurrencyControllerWithCapacity(100)

	// Under normal load
	ctrl.UpdateMetrics(30.0, 40.0)
	if ctrl.CurrentLimit() != 100 {
		t.Fatalf("expected limit 100 under normal load, got %d", ctrl.CurrentLimit())
	}

	// Under moderate load (>80%)
	ctrl.UpdateMetrics(85.0, 50.0)
	if ctrl.CurrentLimit() != 50 {
		t.Fatalf("expected limit 50 under moderate load, got %d", ctrl.CurrentLimit())
	}

	// Under severe load (>90%)
	ctrl.UpdateMetrics(95.0, 92.0)
	expectedSevere := 25
	if ctrl.CurrentLimit() != expectedSevere && ctrl.CurrentLimit() != MinConcurrentConnections {
		t.Fatalf("expected limit %d under severe load, got %d", expectedSevere, ctrl.CurrentLimit())
	}
}

func TestAdaptiveConcurrency_ConcurrentAccess(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctrl := NewAdaptiveConcurrencyControllerWithCapacity(50)
	var wg sync.WaitGroup
	const goroutines = 30
	const iterations = 50

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				if ctrl.AcquireSlot(ctx) {
					// Simulate work
					time.Sleep(100 * time.Microsecond)
					ctrl.ReleaseSlot()
				}
				cancel()
			}
		}()
	}

	wg.Wait()

	if ctrl.Active() != 0 {
		t.Fatalf("expected 0 active slots after concurrent workers finished, got %d", ctrl.Active())
	}
}

func TestAdaptiveConcurrency_EdgeCases(t *testing.T) {
	defer goleak.VerifyNone(t)

	// Zero or negative capacity defaults to 1
	ctrlZero := NewAdaptiveConcurrencyControllerWithCapacity(0)
	if ctrlZero.Capacity() != 1 {
		t.Fatalf("expected capacity 1 for 0 input, got %d", ctrlZero.Capacity())
	}

	// ReleaseSlot on empty controller should not panic or underflow
	ctrlZero.ReleaseSlot()
	if ctrlZero.Active() != 0 {
		t.Fatalf("expected active 0 after release on empty, got %d", ctrlZero.Active())
	}

	// Dynamic limits bounded at minimum
	ctrlSmall := NewAdaptiveConcurrencyControllerWithCapacity(2)
	ctrlSmall.UpdateMetrics(99.0, 99.0)
	if ctrlSmall.CurrentLimit() < 1 {
		t.Fatalf("expected limit >= 1, got %d", ctrlSmall.CurrentLimit())
	}

	// probeAvailableRAM direct check
	ram := probeAvailableRAM()
	if ram <= 0 {
		t.Fatalf("expected positive RAM bytes, got %d", ram)
	}
}

