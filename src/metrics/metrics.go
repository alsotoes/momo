// Package metrics provides the metrics collection and analysis functionality for the momo application.
package metrics

import (
	"context"
	"log"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// SystemMetrics defines an interface for getting system metrics.
// This allows for mocking in tests.
type SystemMetrics interface {
	VirtualMemory() (*mem.VirtualMemoryStat, error)
	CPUPercent() ([]float64, error)
}

// RealSystemMetrics is the implementation of SystemMetrics that uses gopsutil.
type RealSystemMetrics struct{}

// VirtualMemory returns the virtual memory statistics.
func (rsm *RealSystemMetrics) VirtualMemory() (*mem.VirtualMemoryStat, error) {
	return mem.VirtualMemory()
}

// CPUPercent returns the CPU usage percentage.
func (rsm *RealSystemMetrics) CPUPercent() ([]float64, error) {
	return cpu.Percent(0, false)
}

// checkMetricsAndSwap checks the system metrics and determines whether to change the replication mode.
//
// It compares the memory and CPU usage against the configured thresholds and returns the new
// replication index and a boolean indicating whether the mode was changed.
func checkMetricsAndSwap(cfg momo_common.Configuration, sm SystemMetrics, currentIndex int, replicationOrder []int) (int, bool) {
	// ⚡ Bolt: Hoist the currentIndex check to avoid unnecessary system calls.
	if currentIndex == -1 {
		return currentIndex, false
	}

	v, err := sm.VirtualMemory()
	if err != nil {
		log.Printf("Error getting memory metrics: %v", err)
		return currentIndex, false
	}
	// ⚡ Bolt: Avoid redundant divisions by using percentages directly.
	memUsedPercent := v.UsedPercent

	// ⚡ Bolt: Pre-calculate thresholds as percentages.
	maxThresholdPercent := cfg.Metrics.MaxThreshold * 100
	minThresholdPercent := cfg.Metrics.MinThreshold * 100

	// ⚡ Bolt: Short-circuit if memory usage is already high.
	if memUsedPercent >= maxThresholdPercent {
		if currentIndex < len(replicationOrder)-1 {
			log.Printf("Replication changed because memory MaxThreshold reached")
			return currentIndex + 1, true
		}
	}

	c, err := sm.CPUPercent()
	if err != nil {
		log.Printf("Error getting cpu metrics: %v", err)
		return currentIndex, false
	}
	cpuUsedPercent := c[0]

	// Increase replication if CPU usage is high
	if cpuUsedPercent >= maxThresholdPercent {
		if currentIndex < len(replicationOrder)-1 {
			log.Printf("Replication changed because cpu MaxThreshold reached")
			return currentIndex + 1, true
		}
	}

	// Decrease replication if usage is low
	if memUsedPercent < minThresholdPercent && cpuUsedPercent < minThresholdPercent {
		if currentIndex > 0 {
			log.Printf("Replication changed because resource usage is below MinThreshold")
			return currentIndex - 1, true
		}
	}

	return currentIndex, false
}

// GetMetrics is the main loop for the metrics daemon.
//
// It periodically checks the system metrics and, if the polymorphic system is enabled,
// adjusts the replication mode based on the configured thresholds and fallback interval.
// This function is intended to be run as a goroutine and will run indefinitely.
func GetMetrics(ctx context.Context, cfg momo_common.Configuration, serverId int) {
	if serverId != 0 {
		return
	}

	log.Printf("Daemon GetMetrics stated...")

	replicationOrder := cfg.Global.ReplicationOrder
	currentIndex := 0
	pushNewReplicationMode(cfg, replicationOrder[currentIndex])

	sm := &RealSystemMetrics{}
	start := time.Now()

	if !cfg.Global.PolymorphicSystem {
		log.Printf("Replication will not change beacuse polymorphic_system is set to false")
		return
	}

	fallbackDuration := time.Duration(cfg.Metrics.FallbackInterval) * time.Millisecond
	intervalDuration := time.Duration(cfg.Metrics.Interval) * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		newIndex, changed := checkMetricsAndSwap(cfg, sm, currentIndex, replicationOrder)
		if changed {
			currentIndex = newIndex
			pushNewReplicationMode(cfg, replicationOrder[currentIndex])
			start = time.Now()
		}

		// Change replication mode by timeout fallback
		now := time.Now()
		if now.Sub(start) > fallbackDuration {
			if currentIndex > 0 {
				log.Printf("Replication fallback because of timeout")
				currentIndex--
				pushNewReplicationMode(cfg, replicationOrder[currentIndex])
				start = time.Now()
			} else {
				log.Printf("Replication method has no fallback")
			}
		}

		time.Sleep(intervalDuration)
	}
}
