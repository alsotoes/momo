package server

import (
	"testing"

	"github.com/alsotoes/momo/src/common"
)

func TestNewCoordinationHealthMonitorDisabled(t *testing.T) {
	if m := newCoordinationHealthMonitor(common.ConfigurationCoordinationFailover{}, nil); m != nil {
		t.Fatal("disabled configuration must not start a monitor")
	}
}

func TestNewCoordinationHealthMonitorMapsConfig(t *testing.T) {
	m := newCoordinationHealthMonitor(common.ConfigurationCoordinationFailover{
		Enabled: true, Interval: 5, Cooldown: 30,
		CPUThreshold: 75, MemoryThreshold: 80, GoroutineThreshold: 1000,
	}, nil)
	if m == nil {
		t.Fatal("enabled configuration must produce a monitor")
	}
	// Evaluate with no lease manager attached must be a no-op, not a panic.
	if got := m.Evaluate(); got != 0 {
		t.Fatalf("evaluation without a lease manager must resign nothing, got %d", got)
	}
}
