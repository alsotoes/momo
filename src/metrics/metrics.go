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

// HostPressure reports the busiest CPU percentage and memory utilisation, both
// 0-100. Failed samples report zero, so callers leave their limits unchanged
// rather than reacting to missing data (#1145).
func HostPressure(sm SystemMetrics) (cpu, memUsed float64) {
	if v, err := sm.VirtualMemory(); err == nil {
		memUsed = float64(v.UsedPercent)
	}
	if cpus, err := sm.CPUPercent(); err == nil {
		for _, c := range cpus {
			if c > cpu {
				cpu = c
			}
		}
	}
	return cpu, memUsed
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

// ModeStore persists the last replication mode this node observed. It lets a
// restarted or newly-elected controller resume the cluster's mode instead of
// resetting to the first entry of the replication order (Phase 4).
type ModeStore interface {
	LoadMode() (int, bool, error)
	SaveMode(mode int) error
}

// resumeModeIndex returns the index in order matching mode, or 0 when the mode
// is unknown, so a stale persisted value can never wedge the controller.
func resumeModeIndex(order []int, mode int) int {
	for i, m := range order {
		if m == mode {
			return i
		}
	}
	return 0
}

// resumeMode returns the order index matching the persisted mode, falling back
// to 0 when nothing is stored or the value is unreadable/stale.
func resumeMode(order []int, store ModeStore) int {
	if store == nil {
		return 0
	}
	mode, ok, err := store.LoadMode()
	if err != nil {
		log.Printf("AUDIT: could not read persisted replication mode: %v — starting from order[0]", common.SanitizeLog(err.Error()))
		return 0
	}
	if !ok {
		return 0
	}
	idx := resumeModeIndex(order, mode)
	log.Printf("AUDIT: resuming replication mode %d (order index %d) from persisted state", order[idx], idx)
	return idx
}

// persistMode records the selected mode, auditing (not failing) on error: the
// controller must keep working even if the disk is read-only.
func persistMode(order []int, idx int, store ModeStore) {
	if store == nil {
		return
	}
	if err := store.SaveMode(order[idx]); err != nil {
		log.Printf("AUDIT: failed to persist replication mode %d: %v", order[idx], common.SanitizeLog(err.Error()))
	}
}

// GetMetrics is the main loop for the metrics daemon.
//
// It periodically checks the system metrics and, if the polymorphic system is enabled,
// adjusts the replication mode based on the configured thresholds and fallback interval.
// This function is intended to be run as a goroutine and will run indefinitely.
func GetMetrics(ctx context.Context, cfg common.Configuration, serverId int, modeStore ModeStore) {
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

	replicationOrder := cfg.Global.ReplicationOrder
	if len(replicationOrder) == 0 {
		log.Printf("ERROR: replication_order is empty — metrics loop not started")
		return
	}

	var adaptive *AdaptiveThresholds
	if cfg.Adaptive.Enabled {
		a, aErr := NewAdaptiveThresholds(cfg.Adaptive.Alpha, cfg.Adaptive.MinSamples,
			cfg.Metrics.MaxThreshold*100, cfg.Metrics.MinThreshold*100,
			time.Duration(cfg.Adaptive.PersistInterval)*time.Second,
			dataDirFor(cfg.Daemons, serverId))
		if aErr != nil {
			log.Printf("ADAPTIVE: thresholds disabled: %v", common.SanitizeLog(aErr.Error()))
		} else {
			adaptive = a
			setAdaptiveSnapshot(a)
			defer func() {
				setAdaptiveSnapshot(nil)
				_ = a.Close()
			}()
			log.Printf("ADAPTIVE: self-tuning thresholds enabled (alpha=%.2f, min_samples=%d)", cfg.Adaptive.Alpha, cfg.Adaptive.MinSamples)
		}
	}

	c := &metricsController{
		cfg:      cfg,
		adaptive: adaptive,
		// ⚡ Bolt: hoist constant AuthToken padding out of the loop.
		authToken:        []byte(common.PadString(cfg.Global.AuthToken, common.AuthTokenLength)),
		order:            replicationOrder,
		store:            modeStore,
		sm:               &RealSystemMetrics{},
		minFloor:         cfg.Global.MinimumDurabilityFactor,
		clusterSize:      len(cfg.Daemons),
		maxThreshPercent: cfg.Metrics.MaxThreshold * 100,
		minThreshPercent: cfg.Metrics.MinThreshold * 100,
		fallback:         time.Duration(cfg.Metrics.FallbackInterval) * time.Millisecond,
		// Phase 4: resume the mode this node last observed (it receives every
		// push), so a controller restart or takeover does not downgrade.
		currentIndex: resumeMode(replicationOrder, modeStore),
		start:        time.Now(),
	}
	persistMode(replicationOrder, c.currentIndex, modeStore)
	pushNewReplicationMode(cfg, c.authToken, replicationOrder[c.currentIndex])

	ticker := time.NewTicker(time.Duration(cfg.Metrics.Interval) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.step()
		}
	}
}

// metricsController holds the polymorphic loop state so the loop body stays
// readable and within the complexity budget.
type metricsController struct {
	cfg              common.Configuration
	authToken        []byte
	order            []int
	store            ModeStore
	sm               SystemMetrics
	minFloor         int
	clusterSize      int
	maxThreshPercent float64
	minThreshPercent float64
	fallback         time.Duration
	currentIndex     int
	start            time.Time
	adaptive         *AdaptiveThresholds
}

// step evaluates one tick: a metric-driven switch first, then the timeout
// fallback, both respecting the durability floor.
func (c *metricsController) step() {
	maxPct, minPct := c.thresholds()
	if idx, changed := checkMetricsAndSwap(c.sm, c.currentIndex, c.order, maxPct, minPct); changed {
		if shouldSwitchMode(c.minFloor, c.order, idx, c.cfg.Global.ReplicationFactor, c.clusterSize) {
			c.switchTo(idx)
		}
	}
	if time.Since(c.start) > c.fallback {
		c.applyFallback()
	}
}

// thresholds returns the pair in force, feeding the learner when adaptive
// thresholds are enabled.
func (c *metricsController) thresholds() (maxPct, minPct float64) {
	if c.adaptive == nil {
		return c.maxThreshPercent, c.minThreshPercent
	}
	// The loop acts on memory pressure, so that is the signal it learns from.
	if v, err := c.sm.VirtualMemory(); err == nil {
		t := c.adaptive.Observe(float64(v.UsedPercent))
		c.adaptive.MaybePersist(time.Now())
		return t.Max, t.Min
	}
	t := c.adaptive.Thresholds()
	return t.Max, t.Min
}

// switchTo adopts an order index, persists it and broadcasts it.
func (c *metricsController) switchTo(idx int) {
	c.currentIndex = idx
	persistMode(c.order, idx, c.store)
	pushNewReplicationMode(c.cfg, c.authToken, c.order[idx])
	c.start = time.Now()
}

// applyFallback steps one mode down after the interval, unless the durability
// floor forbids it.
func (c *metricsController) applyFallback() {
	if c.currentIndex == 0 {
		log.Printf("Replication method has no fallback")
		return
	}
	if !shouldSwitchMode(c.minFloor, c.order, c.currentIndex-1, c.cfg.Global.ReplicationFactor, c.clusterSize) {
		log.Printf("Replication fallback held because of durability floor")
		c.start = time.Now()
		return
	}
	log.Printf("Replication fallback because of timeout")
	c.switchTo(c.currentIndex - 1)
}
