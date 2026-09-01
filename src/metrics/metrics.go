// Package metrics provides the metrics collection and analysis functionality for the momo application.
package metrics

import (
	"context"
	"log"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// SystemMetrics defines an interface for getting system metrics.
// This allows for mocking in tests.
type SystemMetrics interface {
	// VirtualMemory returns the virtual memory statistics.
	VirtualMemory() (*mem.VirtualMemoryStat, error)
	// CPUPercent returns the CPU usage percentage.
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
func checkMetricsAndSwap(sm SystemMetrics, currentIndex int, replicationOrder []int, maxThreshPercent, minThreshPercent float64) (int, bool) {
	// ⚡ Bolt: Hoist currentIndex == -1 check to avoid unnecessary work.
	if currentIndex == -1 {
		return currentIndex, false
	}

	v, err := sm.VirtualMemory()
	if err != nil {
		log.Printf("Error getting memory metrics: %v", common.SanitizeLog(err.Error()))
		return currentIndex, false
	}

	// ⚡ Bolt: Use pre-calculated thresholds as percentages to match UsedPercent and CPUPercent (0-100)
	// and avoid dividing by 100 on every tick.
	memUsed := float64(v.UsedPercent)

	// ⚡ Bolt: Short-circuit CPUPercent system call if memory alone exceeds the threshold.
	if memUsed >= maxThreshPercent {
		if currentIndex < len(replicationOrder)-1 {
			log.Printf("Replication changed because cfg.Metrics.MaxThreshold reached")
			return currentIndex + 1, true
		}
		return currentIndex, false
	}

	c, err := sm.CPUPercent()
	if err != nil {
		log.Printf("Error getting cpu metrics: %v", common.SanitizeLog(err.Error()))
		return currentIndex, false
	}
	if len(c) == 0 {
		log.Printf("CPUPercent returned empty slice (errno=%d)", syscall.EIO)
		return currentIndex, false
	}
	cpuUsed := c[0]

	// ⚡ Bolt: Use pre-calculated percent thresholds to avoid division.
	if cpuUsed >= maxThreshPercent {
		if currentIndex < len(replicationOrder)-1 {
			log.Printf("Replication changed because cfg.Metrics.MaxThreshold reached")
			return currentIndex + 1, true
		}
	} else if memUsed < minThreshPercent && cpuUsed < minThreshPercent {
		if currentIndex > 0 {
			log.Printf("Replication changed because resource usage is below MinThreshold")
			return currentIndex - 1, true
		}
	}

	return currentIndex, false
}

// effectiveModeDurability returns the achievable replica copy count for a
// replication mode, bounded by cluster size. ReplicationNone holds only the
// source copy (1); all replicated modes hold up to min(replicationFactor,
// clusterSize) copies (issue #822).
func effectiveModeDurability(mode, replicationFactor, clusterSize int) int {
	if mode == common.ReplicationNone {
		return 1
	}
	if clusterSize < 1 {
		clusterSize = 1
	}
	if replicationFactor < clusterSize {
		return replicationFactor
	}
	return clusterSize
}

// shouldSwitchMode reports whether the metrics controller may move to newIdx
// given the durability floor. Escalating to a higher-durability mode is always
// allowed; degrading to a mode whose achievable replica count falls below the
// configured minimum is refused so durability is never silently lost
// (Rule 9 / issue #822). The floor is disabled when <= 0.
func shouldSwitchMode(minFloor int, replicationOrder []int, newIdx, replicationFactor, clusterSize int) bool {
	if minFloor <= 0 {
		return true
	}
	// Refuse any move to a mode whose achievable replica count falls below the
	// floor. This guards the metastable degrade path (checkMetricsAndSwap and
	// the timeout fallback) against silently dropping durability under load.
	if newIdx >= 0 && newIdx < len(replicationOrder) {
		if effectiveModeDurability(replicationOrder[newIdx], replicationFactor, clusterSize) >= minFloor {
			return true
		}
		log.Printf("WARNING: metrics controller refused to switch to mode %d (durability below configured minimum %d) — holding at current mode", replicationOrder[newIdx], minFloor)
		return false
	}
	return true
}

// GetMetrics is the main loop for the metrics daemon.
//
// It periodically checks the system metrics and, if the polymorphic system is enabled,
// adjusts the replication mode based on the configured thresholds and fallback interval.
// This function is intended to be run as a goroutine and will run indefinitely.
func GetMetrics(ctx context.Context, cfg common.Configuration, serverId int) {
	if serverId != 0 {
		return
	}

	if !cfg.Global.PolymorphicSystem {
		log.Printf("Replication will not change because polymorphic_system is set to false")
		return
	}

	log.Printf("Daemon GetMetrics started...")

	if cfg.Metrics.Interval <= 0 {
		log.Printf("ERROR: metrics interval must be positive, got %d — metrics loop not started", cfg.Metrics.Interval)
		return
	}

	// ⚡ Bolt: Hoist constant AuthToken padding and conversion out of the loop.
	paddedAuthToken := []byte(common.PadString(cfg.Global.AuthToken, common.AuthTokenLength))

	// ⚡ Bolt: Pre-calculate thresholds as percentages to avoid multiplication/division in the loop.
	maxThreshPercent := cfg.Metrics.MaxThreshold * 100
	minThreshPercent := cfg.Metrics.MinThreshold * 100

	replicationOrder := cfg.Global.ReplicationOrder
	currentIndex := 0
	pushNewReplicationMode(cfg, paddedAuthToken, replicationOrder[currentIndex])

	// Durability floor (issue #822): the controller must never automatically
	// select a mode whose achievable replica count is below this value.
	minFloor := cfg.Global.MinimumDurabilityFactor
	clusterSize := len(cfg.Daemons)

	sm := &RealSystemMetrics{}
	start := time.Now()

	fallbackDuration := time.Duration(cfg.Metrics.FallbackInterval) * time.Millisecond
	intervalDuration := time.Duration(cfg.Metrics.Interval) * time.Millisecond

	ticker := time.NewTicker(intervalDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			newIndex, changed := checkMetricsAndSwap(sm, currentIndex, replicationOrder, maxThreshPercent, minThreshPercent)
			if changed && shouldSwitchMode(minFloor, replicationOrder, newIndex, cfg.Global.ReplicationFactor, clusterSize) {
				currentIndex = newIndex
				pushNewReplicationMode(cfg, paddedAuthToken, replicationOrder[currentIndex])
				start = time.Now()
			}

			// Change replication mode by timeout fallback
			now := time.Now()
			if now.Sub(start) > fallbackDuration {
				if currentIndex > 0 {
					if shouldSwitchMode(minFloor, replicationOrder, currentIndex-1, cfg.Global.ReplicationFactor, clusterSize) {
						log.Printf("Replication fallback because of timeout")
						currentIndex--
						pushNewReplicationMode(cfg, paddedAuthToken, replicationOrder[currentIndex])
						start = time.Now()
					} else {
						log.Printf("Replication fallback held because of durability floor")
						start = time.Now()
					}
				} else {
					log.Printf("Replication method has no fallback")
				}
			}
		}
	}
}
