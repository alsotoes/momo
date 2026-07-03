package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/shirou/gopsutil/v3/mem"
)

func TestGetMetrics_Cancellation(t *testing.T) {
	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			PolymorphicSystem: true,
			ReplicationOrder: []int{1, 2, 3, 4},
			AuthToken: "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6", // notsecret
		},
		Metrics: common.ConfigurationMetrics{
			Interval: 1,
			MinThreshold: 0.2,
			MaxThreshold: 0.8,
		},
		Daemons: []*common.Daemon{
			{Host: "127.0.0.1:45697", ChangeReplication: "127.0.0.1:45696"},
		},
	}


	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should run and return when context is canceled
	GetMetrics(ctx, cfg, 0)
}

func TestCheckMetricsAndSwap_EdgeCases(t *testing.T) {
	sm := &MockSystemMetrics{
		memStat: &mem.VirtualMemoryStat{UsedPercent: 50.0},
		cpuStat: []float64{50.0},
	}
	
	// Test currentIndex == -1
	idx, changed := checkMetricsAndSwap(sm, -1, []int{1}, 80.0, 20.0)
	if idx != -1 || changed {
		t.Errorf("Expected (-1, false) for currentIndex == -1")
	}
}
