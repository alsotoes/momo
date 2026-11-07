package p2p

import (
	"context"
	"testing"
	"time"
)

func TestHealthLimitsExceeded(t *testing.T) {
	limits := HealthLimits{CPUPercent: 90, MemPercent: 85, Goroutines: 1000}
	cases := []struct {
		name string
		snap HealthSnapshot
		want bool
	}{
		{"cpu", HealthSnapshot{CPUPercent: 95}, true},
		{"memory", HealthSnapshot{MemPercent: 90}, true},
		{"goroutines", HealthSnapshot{Goroutines: 1001}, true},
		{"healthy", HealthSnapshot{CPUPercent: 10, MemPercent: 20, Goroutines: 50}, false},
		{"at limit is fine", HealthSnapshot{CPUPercent: 90, MemPercent: 85, Goroutines: 1000}, false},
		{"unknown cpu does not fire", HealthSnapshot{CPUPercent: 0, MemPercent: 0, Goroutines: 10}, false},
	}
	for _, tc := range cases {
		got, detail := limits.Exceeded(tc.snap)
		if got != tc.want {
			t.Fatalf("%s: exceeded=%v (%s), want %v", tc.name, got, detail, tc.want)
		}
	}
}

func TestHealthLimitsDisabledDimensions(t *testing.T) {
	limits := HealthLimits{} // every threshold zero = disabled
	if bad, _ := limits.Exceeded(HealthSnapshot{CPUPercent: 100, MemPercent: 100, Goroutines: 1e6}); bad {
		t.Fatal("zero thresholds must disable the checks")
	}
}

func TestHealthLimitsHealthyIgnoresStartupSample(t *testing.T) {
	limits := HealthLimits{Goroutines: 100, MinGoroutines: 8}
	if !limits.Healthy(HealthSnapshot{Goroutines: 2}) {
		t.Fatal("a tiny process must not be judged unhealthy")
	}
	if limits.Healthy(HealthSnapshot{Goroutines: 500}) {
		t.Fatal("a large process over the threshold must be unhealthy")
	}
}

func TestHealthMonitorResignsOncePerCooldown(t *testing.T) {
	tr := newFailoverTransport(1, 2)
	lm := NewLeaseManager(1, tr)
	lm.held[1] = &Lease{LeaseID: 1, HolderID: 1, Key: "metrics-controller"}
	lm.held[2] = &Lease{LeaseID: 2, HolderID: 1, Key: "replication-propagator"}

	now := time.Unix(1000, 0)
	m := NewHealthMonitor(HealthMonitorConfig{
		Cooldown: 60 * time.Second,
		Limits:   HealthLimits{CPUPercent: 90},
	}, func() HealthSnapshot { return HealthSnapshot{CPUPercent: 99} }, lm)
	m.now = func() time.Time { return now }

	if got := m.Evaluate(); got != 2 {
		t.Fatalf("expected both leases resigned, got %d", got)
	}
	if lm.ActiveLeases() != 0 {
		t.Fatal("leases must be released on handoff")
	}
	// Still unhealthy, still inside the cooldown: no second resignation.
	lm.held[3] = &Lease{LeaseID: 3, HolderID: 1, Key: "late-lease"}
	if got := m.Evaluate(); got != 0 {
		t.Fatalf("cooldown must suppress repeated resignations, got %d", got)
	}
	now = now.Add(61 * time.Second)
	if got := m.Evaluate(); got != 1 {
		t.Fatalf("after cooldown a new lease must be resigned, got %d", got)
	}
}

func TestHealthMonitorIgnoresHealthySamples(t *testing.T) {
	lm := NewLeaseManager(1, newFailoverTransport(1, 2))
	lm.held[1] = &Lease{LeaseID: 1, HolderID: 1, Key: "k"}
	m := NewHealthMonitor(HealthMonitorConfig{Limits: HealthLimits{CPUPercent: 90}},
		func() HealthSnapshot { return HealthSnapshot{CPUPercent: 5} }, lm)
	if got := m.Evaluate(); got != 0 {
		t.Fatalf("healthy node must not resign, got %d", got)
	}
	if lm.ActiveLeases() != 1 {
		t.Fatal("healthy node must keep its lease")
	}
}

func TestHealthMonitorWithoutLeaseManager(t *testing.T) {
	m := NewHealthMonitor(HealthMonitorConfig{Limits: HealthLimits{CPUPercent: 90}},
		func() HealthSnapshot { return HealthSnapshot{CPUPercent: 99} }, nil)
	if got := m.Evaluate(); got != 0 {
		t.Fatalf("resignation without a manager is a no-op, got %d", got)
	}
}

func TestHealthMonitorRunStopsOnCancel(t *testing.T) {
	m := NewHealthMonitor(HealthMonitorConfig{Interval: time.Millisecond},
		func() HealthSnapshot { return HealthSnapshot{} }, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must return when the context is cancelled")
	}
}
