// Package replication_order implements self-tuning replication-mode ordering
// (issue #1146).
package replication_order

import (
	"sync"
	"time"
)

// Reorderer wraps a Tracker with a stability window: the score-ordered mode
// list is only recomputed when reEvaluateInterval has elapsed since the last
// recomputation. This prevents thrashing the replication order on every
// observation while still converging to the best-performing modes.
type Reorderer struct {
	mu          sync.Mutex
	tracker     *Tracker
	interval    time.Duration
	lastReorder time.Time
	order       []int
}

// NewReorderer builds a reorderer over tracker. A zero or negative interval
// defaults to ten minutes.
func NewReorderer(tracker *Tracker, interval time.Duration) *Reorderer {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	return &Reorderer{
		tracker:  tracker,
		interval: interval,
	}
}

// Order returns the current score-ordered mode list, recomputing it if the
// stability window has elapsed since the last recomputation.
func (r *Reorderer) Order() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.order == nil || time.Since(r.lastReorder) >= r.interval {
		r.order = r.tracker.Order()
		r.lastReorder = time.Now()
	}
	out := make([]int, len(r.order))
	copy(out, r.order)
	return out
}

// Observe forwards a measurement to the underlying tracker. The next Order()
// call recomputes only if the stability window has elapsed.
func (r *Reorderer) Observe(mode int, result ModeResult) {
	r.tracker.Observe(mode, result)
}

// Score returns the latest composite score for mode.
func (r *Reorderer) Score(mode int) float64 {
	return r.tracker.Score(mode)
}

// Stats returns a copy of the latest statistics for mode, or nil when the mode
// has never been observed.
func (r *Reorderer) Stats(mode int) *ModeStats {
	return r.tracker.Stats(mode)
}

// Modes returns the configured modes in their original config order.
func (r *Reorderer) Modes() []int {
	return r.tracker.Modes()
}
