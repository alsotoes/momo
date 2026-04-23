package metrics

import (
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
	for i := 0; i < b.N; i++ {
		checkMetricsAndSwap(cfg, sm, 4, replicationOrder)
	}
}

func BenchmarkCheckMetricsAndSwap_ExtremeHigh(b *testing.B) {
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
			UsedPercent: 95.0,
		},
		cpuStat: []float64{95.0},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checkMetricsAndSwap(cfg, sm, 4, replicationOrder)
	}
}
