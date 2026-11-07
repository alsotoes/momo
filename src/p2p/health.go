package p2p

import (
	"fmt"
	"runtime"
)

// HealthSnapshot is a point-in-time resource sample used to decide whether a
// coordination role holder should hand off proactively (coordinator failover
// Phase 2). CPU and memory percentages are zero when the platform cannot
// report them, and zero values are treated as "unknown" rather than "idle".
type HealthSnapshot struct {
	Goroutines int
	CPUPercent float64
	MemPercent float64
}

// HealthLimits are the thresholds that make a node step down from coordination.
// A zero threshold disables that dimension.
type HealthLimits struct {
	CPUPercent float64
	MemPercent float64
	Goroutines int
	// MinGoroutines keeps the goroutine threshold from firing on a tiny,
	// otherwise healthy process (for example before any work has started).
	MinGoroutines int
}

// Exceeded reports whether the snapshot breaches any enabled threshold and why.
func (l HealthLimits) Exceeded(s HealthSnapshot) (bool, string) {
	if l.CPUPercent > 0 && s.CPUPercent > l.CPUPercent {
		return true, fmt.Sprintf("cpu %.1f%% > %.1f%%", s.CPUPercent, l.CPUPercent)
	}
	if l.MemPercent > 0 && s.MemPercent > l.MemPercent {
		return true, fmt.Sprintf("memory %.1f%% > %.1f%%", s.MemPercent, l.MemPercent)
	}
	if l.Goroutines > 0 && s.Goroutines > l.Goroutines {
		return true, fmt.Sprintf("goroutines %d > %d", s.Goroutines, l.Goroutines)
	}
	return false, ""
}

// Healthy reports whether the snapshot is under every enabled threshold and the
// process has enough goroutines for the sample to be meaningful.
func (l HealthLimits) Healthy(s HealthSnapshot) bool {
	if l.Goroutines > 0 && s.Goroutines < l.MinGoroutines {
		// Too small a process to judge; do not resign on a startup sample.
		return true
	}
	bad, _ := l.Exceeded(s)
	return !bad
}

// HealthSampler returns the current resource snapshot.
type HealthSampler func() HealthSnapshot

// GoroutineHealthSampler is the portable subset: it always reports goroutines
// and leaves CPU/memory unknown. Used on platforms without a /proc reader and
// as the fallback in tests.
func GoroutineHealthSampler() HealthSampler {
	return func() HealthSnapshot { return HealthSnapshot{Goroutines: runtime.NumGoroutine()} }
}
