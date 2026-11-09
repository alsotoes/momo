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

// replicationPool is a per-server-instance buffer pool with its own adaptive
// target. Keeping it out of package globals means two daemons in one process
// (tests, embedded use) cannot resize or drain each other's pool, and a test can
// substitute the backing pool without racing production goroutines.
type replicationPool struct {
	mu      sync.RWMutex
	pool    payloadPoolc
	target  atomic.Int64
	learner *payloadSizeLearner
}

// newReplicationPool builds a pool at the historical minimum capacity.
func newReplicationPool() *replicationPool {
	p := &replicationPool{learner: &payloadSizeLearner{alpha: 0.1}}
	p.target.Store(payloadPoolMinCapacity)
	p.pool = &sync.Pool{New: func() interface{} {
		return make([]byte, p.targetCapacity())
	}}
	return p
}

// targetCapacity returns the capacity the pool currently hands out.
func (p *replicationPool) targetCapacity() int {
	return int(p.target.Load())
}

// get returns a reusable buffer.
func (p *replicationPool) get() []byte {
	p.mu.RLock()
	pool := p.pool
	p.mu.RUnlock()
	return pool.Get().([]byte)
}

// setBackingPool swaps the backing pool (tests).
func (p *replicationPool) setBackingPool(pool payloadPoolc) {
	p.mu.Lock()
	p.pool = pool
	p.mu.Unlock()
}

// recordSize feeds one observed payload length to this pool's learner and
// resizes the target when enough samples have accumulated.
func (p *replicationPool) recordSize(n int) {
	p.learner.observe(n, &p.target)
}

// release returns a buffer only when it still has the pool's current target
// capacity. A buffer grown by append never came from the pool as that slice, so
// returning it would poison the pool with oversized buffers (fix #667).
func (p *replicationPool) release(payload []byte) {
	target := p.targetCapacity()
	if cap(payload) != target {
		return
	}
	p.mu.RLock()
	pool := p.pool
	p.mu.RUnlock()
	pool.Put(payload[:target])
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

// observe records one sample and resizes target when enough accumulated.
func (l *payloadSizeLearner) observe(n int, target *atomic.Int64) {
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
	target.Store(int64(clampCapacity(want)))
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
