package metrics

import (
	"slices"
	"testing"

	momo_common "github.com/alsotoes/momo/src/common"
	"github.com/shirou/gopsutil/v3/mem"
)

func BenchmarkCheckMetricsAndSwap(b *testing.B) {
	cfg := momo_common.Configuration{
		Global: momo_common.ConfigurationGlobal{
			PolymorphicSystem: true,
		},
		Metrics: momo_common.ConfigurationMetrics{
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
		checkMetricsAndSwap(cfg, sm, 4, replicationOrder, maxThreshPercent, minThreshPercent)
	}
}

func BenchmarkIndexSearch(b *testing.B) {
	replicationOrder := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	// Redesign micro-benchmark to do more work per iteration to reduce CI noise
	searchValues := []int{1, 5, 10, -1}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, val := range searchValues {
			index := slices.Index(replicationOrder, val)
			_ = index
		}
	}
}

func BenchmarkIndexDirectTracking(b *testing.B) {
	replicationOrder := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	searchIndices := []int{0, 4, 9, 0} // Mimic the work of the other benchmark
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, idx := range searchIndices {
			_ = replicationOrder[idx]
		}
	}
}
