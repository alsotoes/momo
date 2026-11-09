package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// TestConcurrencyHomeostasisAppliesSampledPressure proves the loop actually
// feeds the controller, which was never called before #1145.
func TestConcurrencyHomeostasisAppliesSampledPressure(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctrl := NewAdaptiveConcurrencyControllerWithCapacity(100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int64
	startConcurrencyHomeostasis(ctx, ctrl, func() (float64, float64) {
		calls.Add(1)
		return 95, 95 // severe pressure
	}, 5*time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ctrl.CurrentLimit() < 100 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ctrl.CurrentLimit() >= 100 {
		t.Fatalf("limit stayed at capacity under severe pressure: %d", ctrl.CurrentLimit())
	}
	if calls.Load() == 0 {
		t.Fatal("sampler was never called")
	}
	cancel()
}

func TestConcurrencyHomeostasisNoOps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startConcurrencyHomeostasis(ctx, nil, func() (float64, float64) { return 0, 0 }, time.Second)
	startConcurrencyHomeostasis(ctx, NewAdaptiveConcurrencyControllerWithCapacity(10), nil, time.Second)
	startConcurrencyHomeostasis(ctx, NewAdaptiveConcurrencyControllerWithCapacity(10), func() (float64, float64) { return 0, 0 }, 0)
}
