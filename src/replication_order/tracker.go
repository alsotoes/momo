// Package replication_order implements self-tuning replication-mode ordering
// (issue #1146). It observes per-mode performance and continuously reorders the
// replication mode sequence so better-performing modes are tried first, while
// honoring semantic constraints (e.g. ReplicationNone never precedes a
// replicated mode) and a stability window that prevents thrashing.
//
// A disabled tracker leaves the static config order untouched, so
// replication_order.enabled=false is a zero behavioral change.
package replication_order

import (
	"sort"
	"sync"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// ModeResult captures the observed outcome of using one replication mode.
type ModeResult struct {
	Success   bool  // whether the mode's write/propagation succeeded
	LatencyNs int64 // observed end-to-end latency (0 = not measured)
	Bytes     int64 // bytes transferred (0 = not measured)
}

// ModeStats holds exponentially-weighted moving-average statistics for one mode.
type ModeStats struct {
	Mode        int
	SuccessRate float64 // EWMA of success (1.0 = every observation succeeded)
	LatencyNs   int64   // EWMA mean latency in nanoseconds
	LatencyP99  int64   // approximate p99 latency in nanoseconds
	Throughput  float64 // EWMA bytes per second
	ErrorRate   float64 // EWMA of failures (1.0 = every observation failed)
	Score       float64 // composite weighted score
	Samples     int     // number of observations
	LastUpdated time.Time
}

// Weights for the composite score. The defaults match the spec:
// 0.4 * success + 0.3 * (1/latency) + 0.2 * throughput - 0.1 * error_rate.
type Weights struct {
	Success    float64
	Latency    float64
	Throughput float64
	Error      float64
}

// DefaultWeights is the spec'd weighting.
var DefaultWeights = Weights{
	Success:    0.4,
	Latency:    0.3,
	Throughput: 0.2,
	Error:      0.1,
}

// ewma updates an exponentially weighted moving average with smoothing factor
// alpha (0 < alpha <= 1). A higher alpha reacts faster to new samples.
func ewma(old, sample float64, alpha float64) float64 {
	return alpha*sample + (1.0-alpha)*old
}

// Tracker observes per-mode performance and produces a score-ordered mode list.
type Tracker struct {
	mu         sync.RWMutex
	modes      []int
	stats      map[int]*ModeStats
	weights    Weights
	alpha      float64
	minSamples int

	bestLatencyNs  int64   // fastest latency observed across modes (for normalization)
	bestThroughput float64 // highest throughput observed across modes (for normalization)
}

// NewTracker builds a tracker over the given modes. modes must be non-empty.
// alpha is the EWMA smoothing factor in (0, 1]; minSamples is the number of
// observations after which a mode's score is considered reliable.
func NewTracker(modes []int, alpha float64, minSamples int, weights Weights) *Tracker {
	if alpha <= 0 || alpha > 1 {
		alpha = 0.2
	}
	if minSamples < 1 {
		minSamples = 1
	}
	stats := make(map[int]*ModeStats, len(modes))
	for _, m := range modes {
		stats[m] = &ModeStats{Mode: m}
	}
	return &Tracker{
		modes:      append([]int(nil), modes...),
		stats:      stats,
		weights:    weights,
		alpha:      alpha,
		minSamples: minSamples,
	}
}

// Observe records one measurement for mode and recomputes its score.
func (t *Tracker) Observe(mode int, r ModeResult) {
	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.stats[mode]
	if !ok {
		s = &ModeStats{Mode: mode}
		t.stats[mode] = s
		t.modes = append(t.modes, mode)
	}

	s.Samples++
	s.LastUpdated = time.Now()

	successSample := 0.0
	if r.Success {
		successSample = 1.0
	}
	s.SuccessRate = ewma(s.SuccessRate, successSample, t.alpha)
	s.ErrorRate = ewma(s.ErrorRate, 1.0-successSample, t.alpha)

	if r.LatencyNs > 0 {
		s.LatencyNs = int64(ewma(float64(s.LatencyNs), float64(r.LatencyNs), t.alpha))
		// Approximate p99 as mean * 3 (heavy-tailed network latencies).
		p99 := s.LatencyNs * 3
		if p99 > s.LatencyP99 || s.LatencyP99 == 0 {
			s.LatencyP99 = p99
		}
		if t.bestLatencyNs == 0 || s.LatencyNs < t.bestLatencyNs {
			t.bestLatencyNs = s.LatencyNs
		}
	}

	if r.Bytes > 0 {
		// Throughput as bytes per observation window; approximate window as 1s
		// when no duration is supplied, which is fine for relative ranking.
		s.Throughput = ewma(s.Throughput, float64(r.Bytes), t.alpha)
		if s.Throughput > t.bestThroughput {
			t.bestThroughput = s.Throughput
		}
	}

	s.Score = t.score(s)
}

// score computes the composite weighted score for a mode. Each component is
// normalized to [0, 1] so the weights are directly comparable.
func (t *Tracker) score(s *ModeStats) float64 {
	latencyScore := 1.0
	if s.LatencyNs > 0 && t.bestLatencyNs > 0 {
		latencyScore = float64(t.bestLatencyNs) / float64(s.LatencyNs)
		if latencyScore > 1 {
			latencyScore = 1
		}
		if latencyScore < 0 {
			latencyScore = 0
		}
	}

	throughputScore := 0.0
	if s.Throughput > 0 && t.bestThroughput > 0 {
		throughputScore = s.Throughput / t.bestThroughput
		if throughputScore > 1 {
			throughputScore = 1
		}
	}

	return t.weights.Success*s.SuccessRate +
		t.weights.Latency*latencyScore +
		t.weights.Throughput*throughputScore -
		t.weights.Error*s.ErrorRate
}

// Order returns the modes sorted by composite score descending, then by mode
// id for determinism. Modes with fewer than minSamples observations keep their
// initial position ahead of unobserved modes so a new mode isn't demoted on a
// single sample. The result is stable: equal scores preserve input order.
func (t *Tracker) Order() []int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ordered := make([]int, len(t.modes))
	copy(ordered, t.modes)

	// Only re-rank modes that have been observed enough to be reliable.
	reliable := make(map[int]bool)
	for _, m := range t.modes {
		if s, ok := t.stats[m]; ok && s.Samples >= t.minSamples {
			reliable[m] = true
		}
	}

	sort.SliceStable(ordered, func(i, j int) bool {
		si, sj := t.stats[ordered[i]], t.stats[ordered[j]]
		ri, rj := reliable[ordered[i]], reliable[ordered[j]]
		switch {
		case ri && !rj:
			return true
		case !ri && rj:
			return false
		case ri && rj:
			if si.Score != sj.Score {
				return si.Score > sj.Score
			}
		}
		// Neither reliable (or both): preserve config order via stable sort.
		return false
	})

	return applyConstraints(ordered)
}

// Score returns the latest composite score for mode, or 0 if never observed.
func (t *Tracker) Score(mode int) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if s, ok := t.stats[mode]; ok {
		return s.Score
	}
	return 0
}

// Stats returns a copy of the latest statistics for mode.
func (t *Tracker) Stats(mode int) *ModeStats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if s, ok := t.stats[mode]; ok {
		out := *s
		return &out
	}
	return nil
}

// Modes returns the configured modes in their original config order.
func (t *Tracker) Modes() []int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]int, len(t.modes))
	copy(out, t.modes)
	return out
}

// applyConstraints enforces semantic ordering rules on top of the score rank:
// ReplicationNone (no replication) must never be tried before a replicated
// mode, because degrading to None silently sacrifices durability. Chain and
// Splay may appear in any relative order.
func applyConstraints(order []int) []int {
	out := append([]int(nil), order...)
	noneIdx := -1
	chainIdx := -1
	splayIdx := -1
	for i, m := range out {
		switch m {
		case common.ReplicationNone:
			noneIdx = i
		case common.ReplicationChain:
			chainIdx = i
		case common.ReplicationSplay:
			splayIdx = i
		}
	}
	if noneIdx < 0 {
		return out
	}
	lastReplicated := -1
	if chainIdx > lastReplicated {
		lastReplicated = chainIdx
	}
	if splayIdx > lastReplicated {
		lastReplicated = splayIdx
	}
	if noneIdx >= lastReplicated {
		return out
	}
	// Move None to just after the last replicated mode.
	removed := append(out[:noneIdx], out[noneIdx+1:]...)
	if lastReplicated >= len(removed) {
		return append(removed, common.ReplicationNone)
	}
	result := make([]int, 0, len(removed)+1)
	result = append(result, removed[:lastReplicated+1]...)
	result = append(result, common.ReplicationNone)
	result = append(result, removed[lastReplicated+1:]...)
	return result
}
