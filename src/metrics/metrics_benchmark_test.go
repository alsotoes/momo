package metrics

import (
	"slices"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"github.com/shirou/gopsutil/v3/mem"
)

func BenchmarkCheckMetricsAndSwap(b *testing.B) {
	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			PolymorphicSystem: true,
		},
		Metrics: common.ConfigurationMetrics{
			MinThreshold: 0.2,
			MaxThreshold: 0.8,
		},
	}

	replicationOrder := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	sm := &MockSystemMetrics{
		memStat: &mem.VirtualMemoryStat{
			UsedPercent: 50.0,
		},
		cpuStat: []float64{50.0},
	}

	b.ResetTimer()
	maxThreshPercent := cfg.Metrics.MaxThreshold * 100
	minThreshPercent := cfg.Metrics.MinThreshold * 100
	for i := 0; i < b.N; i++ {
		checkMetricsAndSwap(sm, 4, replicationOrder, maxThreshPercent, minThreshPercent)
	}
}

func BenchmarkIndexSearch(b *testing.B) {
	replicationOrder := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	currentReplicationMode := 5
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		index := slices.Index(replicationOrder, currentReplicationMode)
		_ = index
	}
}

func BenchmarkIndexDirectTracking(b *testing.B) {
	replicationOrder := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	currentIndex := 4 // tracks currentReplicationMode = 5
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = replicationOrder[currentIndex]
	}
}
