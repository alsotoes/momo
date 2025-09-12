// Package momo provides the metrics collection and analysis functionality for the momo application.
package momo

import (
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
// replication mode and a boolean indicating whether the mode was changed.
func checkMetricsAndSwap(cfg momo_common.Configuration, sm SystemMetrics, currentReplicationMode int, replicationOrder []int) (int, bool) {
	v, err := sm.VirtualMemory()
	if err != nil {
		log.Printf("Error getting memory metrics: %v", err)
		return currentReplicationMode, false
	}
	memUsed := float64(v.UsedPercent) / 100

	c, err := sm.CPUPercent()
	if err != nil {
		log.Printf("Error getting cpu metrics: %v", err)
		return currentReplicationMode, false
	}
	cpuUsed := c[0] / 100

	index := -1
	for i, v := range replicationOrder {
		if v == currentReplicationMode {
			index = i
			break
		}
	}

	if index != -1 {
		// Increase replication if usage is high
		if memUsed >= cfg.Metrics.MaxThreshold || cpuUsed >= cfg.Metrics.MaxThreshold {
			if index < len(replicationOrder)-1 {
				log.Printf("Replication changed because cfg.Metrics.MaxThreshold reached")
				return replicationOrder[index+1], true
			}
		}

		// Decrease replication if usage is low
		if memUsed < cfg.Metrics.MinThreshold && cpuUsed < cfg.Metrics.MinThreshold {
			if index > 0 {
				log.Printf("Replication changed because resource usage is below MinThreshold")
				return replicationOrder[index-1], true
			}
		}
	}

	return currentReplicationMode, false
}

// GetMetrics is the main loop for the metrics daemon.
//
// It periodically checks the system metrics and, if the polymorphic system is enabled,
// adjusts the replication mode based on the configured thresholds and fallback interval.
func GetMetrics(cfg momo_common.Configuration, serverId int) {
	if serverId != 0 {
		return
	}

	log.Printf("Daemon GetMetrics stated...")

	replicationOrder := cfg.Global.ReplicationOrder
	currentReplicationMode := replicationOrder[0]
	pushNewReplicationMode(currentReplicationMode)

	sm := &RealSystemMetrics{}
	start := time.Now()

	for {
		if cfg.Global.PolymorphicSystem {
			newReplicationMode, changed := checkMetricsAndSwap(cfg, sm, currentReplicationMode, replicationOrder)
			if changed {
				currentReplicationMode = newReplicationMode
				pushNewReplicationMode(currentReplicationMode)
				start = time.Now()
			}

			// Change replication mode by timeout fallback
			now := time.Now()
			index := -1
			for i, v := range replicationOrder {
				if v == currentReplicationMode {
					index = i
					break
				}
			}
			if now.Sub(start) > (time.Duration(cfg.Metrics.FallbackInterval) * time.Millisecond) {
				if index != -1 && index > 0 {
					log.Printf("Replication fallback because of timeout")
					currentReplicationMode = replicationOrder[index-1]
					pushNewReplicationMode(currentReplicationMode)
					start = time.Now()
				} else {
					log.Printf("Replication method has no fallback")
				}
			}

			time.Sleep(time.Duration(cfg.Metrics.Interval) * time.Millisecond)
		} else {
			log.Printf("Replication will not change beacuse polymorphic_system is set to false")
			return
		}
	}
}
