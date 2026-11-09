package server

import (
	"math"
	"sync"
	"sync/atomic"
)

// Adaptive replication-buffer sizing (Phase 4 of adaptive-thresholds-v1, #1145).
//
// Cluster-wide broadcasts reuse one pooled buffer per P. The useful capacity is
// whatever real replication payloads need, which varies with cluster size and
// metadata volume; a fixed 1 KiB buffer is reallocated on almost every send.
// The pool learns the observed payload size and resizes itself within bounds.
const (
	// payloadPoolMinCapacity is the floor: the historical fixed size.
	payloadPoolMinCapacity = payloadPoolCapacity
	// payloadPoolMaxCapacity bounds per-P memory: a 1 MiB buffer on 32 Ps would
	// otherwise reserve 32 MiB for a rarely-hit case.
	payloadPoolMaxCapacity = 1 << 20
	// payloadPoolSamplesPerResize avoids reacting to every single send.
	payloadPoolSamplesPerResize = 64
	// payloadPoolHeadroom covers payload growth between samples.
	payloadPoolHeadroom = 1.5
)

// payloadPoolTarget is the capacity the pool currently hands out. Read on the
// Get/release paths, so it is atomic rather than mutex-guarded.
var payloadPoolTarget atomic.Int64

func init() {
	payloadPoolTarget.Store(payloadPoolMinCapacity)
}

// payloadPoolTargetCapacity returns the current pool capacity.
func payloadPoolTargetCapacity() int {
	return int(payloadPoolTarget.Load())
}

// payloadSizeLearner folds observed payload lengths into a smoothed mean and
// resizes the pool periodically.
type payloadSizeLearner struct {
	mu       sync.Mutex
	alpha    float64
	samples  int
	since    int
	mean     float64
	observed int
}

// package-level learner for the replication path.
var poolSizing = &payloadSizeLearner{alpha: 0.1}

// recordReplicationPayloadSize feeds one payload length to the learner and
// resizes the pool when enough samples accumulated.
func recordReplicationPayloadSize(n int) {
	poolSizing.observe(n)
}

// observe records one sample.
func (l *payloadSizeLearner) observe(n int) {
	if n <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.samples == 0 {
		l.mean = float64(n)
	} else {
		l.mean += l.alpha * (float64(n) - l.mean)
	}
	l.samples++
	l.observed++
	l.since++
	if l.since < payloadPoolSamplesPerResize {
		return
	}
	l.since = 0
	want := int(math.Round(l.mean * payloadPoolHeadroom))
	payloadPoolTarget.Store(int64(clampCapacity(want)))
}

// meanAndSamples exposes learner state for tests.
func (l *payloadSizeLearner) meanAndSamples() (float64, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.mean, l.samples
}

// clampCapacity bounds a candidate pool capacity to [min, max].
func clampCapacity(v int) int {
	if v < payloadPoolMinCapacity {
		return payloadPoolMinCapacity
	}
	if v > payloadPoolMaxCapacity {
		return payloadPoolMaxCapacity
	}
	return v
}
