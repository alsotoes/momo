package metrics

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

	replicationOrder := []int{1, 2, 3}

	tests := []struct {
		name            string
		currentIndex    int
		memUsedPercent  float64
		cpuUsedPercent  float64
		expectedIndex   int
		expectedChanged bool
	}{
		{
			name:            "Should not change when metrics are normal",
			currentIndex:    1, // Points to 2 in [1, 2, 3]
			memUsedPercent:  50.0,
			cpuUsedPercent:  50.0,
			expectedIndex:   1,
			expectedChanged: false,
		},
		{
			name:            "Should increase replication mode when usage is high",
			currentIndex:    1,
			memUsedPercent:  90.0,
			cpuUsedPercent:  50.0,
			expectedIndex:   2,
			expectedChanged: true,
		},
		{
			name:            "Should decrease replication mode when usage is low",
			currentIndex:    1,
			memUsedPercent:  10.0, // Triggers memFree <= minThreshold
			cpuUsedPercent:  10.0,
			expectedIndex:   0,
			expectedChanged: true,
		},
		{
			name:            "Should not increase replication mode at max level",
			currentIndex:    2,
			memUsedPercent:  90.0,
			cpuUsedPercent:  90.0,
			expectedIndex:   2,
			expectedChanged: false,
		},
		{
			name:            "Should not decrease replication mode at min level",
			currentIndex:    0,
			memUsedPercent:  10.0,
			cpuUsedPercent:  10.0,
			expectedIndex:   0,
			expectedChanged: false,
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

			newIndex, changed := checkMetricsAndSwap(cfg, sm, tt.currentIndex, replicationOrder)

			if newIndex != tt.expectedIndex {
				t.Errorf("Expected index %d, got %d", tt.expectedIndex, newIndex)
			}
			if changed != tt.expectedChanged {
				t.Errorf("Expected changed status %v, got %v", tt.expectedChanged, changed)
			}
		})
	}
}
