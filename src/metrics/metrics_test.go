package momo

import (
	"testing"

	momo_common "github.com/alsotoes/momo/src/common"
	"github.com/shirou/gopsutil/v3/mem"
)

// MockSystemMetrics is a mock implementation of the SystemMetrics interface for testing.
type MockSystemMetrics struct {
	memStat *mem.VirtualMemoryStat
	cpuStat []float64
}

func (msm *MockSystemMetrics) VirtualMemory() (*mem.VirtualMemoryStat, error) {
	return msm.memStat, nil
}

func (msm *MockSystemMetrics) CPUPercent() ([]float64, error) {
	return msm.cpuStat, nil
}

func TestCheckMetricsAndSwap(t *testing.T) {
	cfg := momo_common.Configuration{
		Global: momo_common.ConfigurationGlobal{
			PolymorphicSystem: true,
		},
		Metrics: momo_common.ConfigurationMetrics{
			MinThreshold: 0.2,
			MaxThreshold: 0.8,
		},
	}

	replicationOrder := []string{"1", "2", "3"}

	tests := []struct {
		name                   string
		currentReplicationMode int
		memUsedPercent         float64
		cpuUsedPercent         float64
		expectedReplicationMode int
		expectedChanged        bool
	}{
		{
			name:                   "Should not change when metrics are normal",
			currentReplicationMode: 2,
			memUsedPercent:         50.0,
			cpuUsedPercent:         50.0,
			expectedReplicationMode: 2,
			expectedChanged:        false,
		},
		{
			name:                   "Should increase replication mode when usage is high",
			currentReplicationMode: 2,
			memUsedPercent:         90.0,
			cpuUsedPercent:         50.0,
			expectedReplicationMode: 3,
			expectedChanged:        true,
		},
		{
			name:                   "Should decrease replication mode when usage is low",
			currentReplicationMode: 2,
			memUsedPercent:         10.0, // Triggers memFree <= minThreshold
			cpuUsedPercent:         10.0,
			expectedReplicationMode: 1,
			expectedChanged:        true,
		},
		{
			name:                   "Should not increase replication mode at max level",
			currentReplicationMode: 3,
			memUsedPercent:         90.0,
			cpuUsedPercent:         90.0,
			expectedReplicationMode: 3,
			expectedChanged:        false,
		},
		{
			name:                   "Should not decrease replication mode at min level",
			currentReplicationMode: 1,
			memUsedPercent:         10.0,
			cpuUsedPercent:         10.0,
			expectedReplicationMode: 1,
			expectedChanged:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := &MockSystemMetrics{
				memStat: &mem.VirtualMemoryStat{
					UsedPercent: tt.memUsedPercent,
				},
				cpuStat: []float64{tt.cpuUsedPercent},
			}

			newReplicationMode, changed := checkMetricsAndSwap(cfg, sm, tt.currentReplicationMode, replicationOrder)

			if newReplicationMode != tt.expectedReplicationMode {
				t.Errorf("Expected replication mode %d, got %d", tt.expectedReplicationMode, newReplicationMode)
			}
			if changed != tt.expectedChanged {
				t.Errorf("Expected changed status %v, got %v", tt.expectedChanged, changed)
			}
		})
	}
}
