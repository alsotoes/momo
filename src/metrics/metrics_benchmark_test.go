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

	replicationOrder := []int{3, 2, 1}
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
		checkMetricsAndSwap(sm, 2, replicationOrder, maxThreshPercent, minThreshPercent)
	}
}

func BenchmarkIndexSearch(b *testing.B) {
	replicationOrder := []int{3, 2, 1}
	currentReplicationMode := 2
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		index := slices.Index(replicationOrder, currentReplicationMode)
		_ = index
	}
}

func BenchmarkIndexDirectTracking(b *testing.B) {
	replicationOrder := []int{3, 2, 1}
	currentIndex := 1 // tracks currentReplicationMode = 2
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = replicationOrder[currentIndex]
	}
}
