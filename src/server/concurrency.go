package server

import (
	"context"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
)

const (
	// MinConcurrentConnections is the absolute floor for connection concurrency.
	MinConcurrentConnections = 32
	// MaxConcurrentConnections is the upper safety bound on bare-metal systems.
	MaxConcurrentConnections = 10000
	// DefaultConcurrentLimit is the fallback when environment probing fails.
	DefaultConcurrentLimit = 1000
	// EstimatedPerConnRAMBytes estimates the buffer and stack memory per streaming connection.
	EstimatedPerConnRAMBytes = 4 * 1024 * 1024 // 4 MiB
)

// AdaptiveConcurrencyController defines the compile-time Go interface seam (Rule 74)
// for epigenetic environment probing and homeostatic connection admission control.
type AdaptiveConcurrencyController interface {
	// AcquireSlot requests a connection slot. It blocks until a slot is available
	// or ctx is canceled. Returns true if acquired, false if canceled or throttled.
	AcquireSlot(ctx context.Context) bool

	// ReleaseSlot releases a previously acquired connection slot.
	ReleaseSlot()

	// Capacity returns the calculated maximum connection ceiling from epigenetic probing.
	Capacity() int

	// CurrentLimit returns the dynamically adjusted admission limit under load.
	CurrentLimit() int

	// Active returns the number of currently active connection slots.
	Active() int

	// UpdateMetrics adjusts the dynamic admission limit homeostatically based on host load.
	UpdateMetrics(cpuPercent, memUsedPercent float64)
}

// epigeneticController implements AdaptiveConcurrencyController.
type epigeneticController struct {
	capacity     int
	currentLimit atomic.Int64
	active       atomic.Int64
	sem          chan struct{}
}

// NewAdaptiveConcurrencyController probes the host environment on startup and returns
// an initialized AdaptiveConcurrencyController.
func NewAdaptiveConcurrencyController() AdaptiveConcurrencyController {
	capLimit := probeEnvironment()
	ctrl := &epigeneticController{
		capacity: capLimit,
		sem:      make(chan struct{}, capLimit),
	}
	ctrl.currentLimit.Store(int64(capLimit))
	return ctrl
}

// NewAdaptiveConcurrencyControllerWithCapacity creates a controller with an explicit
// capacity for testing and isolated simulation.
func NewAdaptiveConcurrencyControllerWithCapacity(capLimit int) AdaptiveConcurrencyController {
	if capLimit < 1 {
		capLimit = 1
	}
	ctrl := &epigeneticController{
		capacity: capLimit,
		sem:      make(chan struct{}, capLimit),
	}
	ctrl.currentLimit.Store(int64(capLimit))
	return ctrl
}

// Capacity returns the epigenetic maximum connection capacity.
func (c *epigeneticController) Capacity() int {
	return c.capacity
}

// CurrentLimit returns the active homeostatic connection limit.
func (c *epigeneticController) CurrentLimit() int {
	return int(c.currentLimit.Load())
}

// Active returns the number of active connection handlers.
func (c *epigeneticController) Active() int {
	return int(c.active.Load())
}

// AcquireSlot reserves a connection slot, observing homeostatic load shedding.
func (c *epigeneticController) AcquireSlot(ctx context.Context) bool {
	// 🛡️ Zero-Crash: Unified panic recovery (Rule 37).
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in AcquireSlot: %v", r)
		}
	}()

	// Homeostatic check: shed connections if active count exceeds dynamic threshold
	if c.active.Load() >= c.currentLimit.Load() {
		return false
	}

	select {
	case c.sem <- struct{}{}:
		c.active.Add(1)
		return true
	case <-ctx.Done():
		return false
	}
}

// ReleaseSlot frees a connection slot upon handler completion.
func (c *epigeneticController) ReleaseSlot() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in ReleaseSlot: %v", r)
		}
	}()

	select {
	case <-c.sem:
		c.active.Add(-1)
	default:
	}
}

// UpdateMetrics homeostatically scales the connection limit under system pressure.
func (c *epigeneticController) UpdateMetrics(cpuPercent, memUsedPercent float64) {
	limit := int64(c.capacity)

	if cpuPercent > 90.0 || memUsedPercent > 90.0 {
		// Severe load: throttle down to 25% of capacity
		limit = int64(c.capacity) / 4
	} else if cpuPercent > 80.0 || memUsedPercent > 80.0 {
		// Moderate load: throttle down to 50% of capacity
		limit = int64(c.capacity) / 2
	}

	if limit < MinConcurrentConnections && c.capacity >= MinConcurrentConnections {
		limit = MinConcurrentConnections
	}
	if limit < 1 {
		limit = 1
	}

	c.currentLimit.Store(limit)
}

// probeEnvironment evaluates host constraints (FDs, cgroups, memory) to size the daemon.
func probeEnvironment() int {
	// 1. Probing file descriptor limit
	fdLimit := 1024
	var rlim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlim); err == nil && rlim.Cur > 0 {
		fdLimit = int(rlim.Cur)
	}

	// 2. Probing available memory
	usableRAM := probeAvailableRAM()

	// 3. Compute epigenetic ceiling
	fdCeiling := fdLimit / 4
	if fdCeiling < MinConcurrentConnections {
		fdCeiling = MinConcurrentConnections
	}

	ramCeiling := int(usableRAM / EstimatedPerConnRAMBytes)
	if ramCeiling < MinConcurrentConnections {
		ramCeiling = MinConcurrentConnections
	}

	capLimit := fdCeiling
	if ramCeiling < capLimit {
		capLimit = ramCeiling
	}

	// Clamp to [MinConcurrentConnections, MaxConcurrentConnections]
	if capLimit < MinConcurrentConnections {
		capLimit = MinConcurrentConnections
	} else if capLimit > MaxConcurrentConnections {
		capLimit = MaxConcurrentConnections
	}

	return capLimit
}

// probeAvailableRAM checks container cgroup limits or falls back to system runtime memory.
func probeAvailableRAM() int64 {
	// Check cgroups v2
	if data, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		val := strings.TrimSpace(string(data))
		if val != "max" {
			if limit, err := strconv.ParseInt(val, 10, 64); err == nil && limit > 0 {
				return limit
			}
		}
	}

	// Check cgroups v1
	if data, err := os.ReadFile("/sys/fs/cgroup/memory/memory.limit_in_bytes"); err == nil {
		val := strings.TrimSpace(string(data))
		if limit, err := strconv.ParseInt(val, 10, 64); err == nil && limit > 0 && limit < (1<<62) {
			return limit
		}
	}

	// Fallback to runtime memory inspection
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	if memStats.Sys > 0 {
		return int64(memStats.Sys) * 4
	}

	// Safe fallback (4 GiB assumed)
	return 4 * 1024 * 1024 * 1024
}
