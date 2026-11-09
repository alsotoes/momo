package client

import (
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"sync"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// Pheromone constants for Ant Colony Optimization (§3 in ADAPTIVE_SYSTEMS.md).
const (
	// InitialPheromone is assigned to freshly discovered or untested nodes.
	InitialPheromone = 1.0
	// MinPheromone sets the absolute floor to prevent node starvation.
	MinPheromone = 0.05
	// MaxPheromone bounds the highest reputation a node can accumulate.
	MaxPheromone = 10.0
	// EvaporationFactor decays historical trails over time (tau *= 0.95).
	EvaporationFactor = 0.95
	// FailurePenaltyFactor aggressively reduces the trail upon dial or transfer failure (tau *= 0.2).
	FailurePenaltyFactor = 0.2
	// EvaporationInterval controls how frequently lazy evaporation triggers.
	EvaporationInterval = 1 * time.Second
)

// Adaptive pheromone tuning bounds (Phase 3 of adaptive-thresholds-v1, #1145).
// A volatile network should forget stale trails quickly; a stable one should
// keep them. Values are clamped so learning cannot disable evaporation or turn
// a single failure into a permanent ban.
const (
	// EvaporationFloor is the fastest decay a volatile network may learn.
	EvaporationFloor = 0.90
	// EvaporationCeiling is the slowest decay a stable network may learn.
	EvaporationCeiling = 0.99
	// PenaltyFloor is the harshest failure penalty (a harder reset).
	PenaltyFloor = 0.10
	// PenaltyCeiling is the mildest failure penalty.
	PenaltyCeiling = 0.50
	// tuningMinSamples is how many RTT observations are needed before learned
	// evaporation replaces the constant.
	tuningMinSamples = 20
)

// PheromoneRouter defines the compile-time Go interface seam (Rule 74) for
// adaptive replica selection inspired by ant colony foraging trails.
type PheromoneRouter interface {
	// SelectReplica chooses the best replica candidate from eligible CRUSH nodes
	// using weighted probabilistic selection (roulette-wheel).
	SelectReplica(replicas []*common.Node) (*common.Node, error)

	// PrioritizeReplicas returns the candidates ordered by weighted reputation,
	// allowing resilient fallback from most preferred to least preferred.
	PrioritizeReplicas(replicas []*common.Node) ([]*common.Node, error)

	// RecordSuccess strengthens the pheromone trail of nodeID inversely proportional to RTT.
	RecordSuccess(nodeID int, rtt time.Duration)

	// RecordFailure severely penalizes the pheromone trail of nodeID.
	RecordFailure(nodeID int)

	// Evaporate applies exponential decay across all tracked pheromone trails.
	Evaporate()

	// GetPheromone returns the current pheromone level for nodeID.
	GetPheromone(nodeID int) float64
}

// antColonyRouter is the concrete, thread-safe implementation of PheromoneRouter.
type antColonyRouter struct {
	mu            sync.RWMutex
	pheromones    map[int]float64
	lastEvaporate time.Time

	// Adaptive tuning state (Phase 3, #1145). Defaults are the package
	// constants; learning moves them within the clamp bounds.
	evaporation float64
	penalty     float64
	// RTT statistics: EWMA mean and variance over successful transfers.
	rttMean     float64
	rttVariance float64
	rttSamples  int
	failures    uint64
	attempts    uint64
}

// NewPheromoneRouter creates a new Ant Colony PheromoneRouter.
func NewPheromoneRouter() PheromoneRouter {
	return &antColonyRouter{
		pheromones:    make(map[int]float64),
		lastEvaporate: time.Now(),
		evaporation:   EvaporationFactor,
		penalty:       FailurePenaltyFactor,
	}
}

// DefaultRouter is a shared, package-level PheromoneRouter singleton for client downloads.
var DefaultRouter = NewPheromoneRouter()

// RecordSuccess strengthens the trail: fast completions yield higher rewards.
func (r *antColonyRouter) RecordSuccess(nodeID int, rtt time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cur, ok := r.pheromones[nodeID]
	if !ok {
		cur = InitialPheromone
	}

	// Reward formula: faster RTT yields greater reward, clamped to [0.1, 2.0].
	reward := 0.5
	if rtt > 0 {
		// 100ms baseline: 10ms -> 2.0 reward; 200ms -> 0.5 reward; 1s -> 0.1 reward
		reward = 100.0 / float64(rtt.Milliseconds()+1)
		if reward > 2.0 {
			reward = 2.0
		} else if reward < 0.1 {
			reward = 0.1
		}
	}

	r.observeRTTLocked(rtt)
	cur += reward
	if cur > MaxPheromone {
		cur = MaxPheromone
	}
	r.pheromones[nodeID] = cur
}

// RecordFailure applies a severe penalty (tau *= 0.2) to rapidly shed traffic from a sick node.
func (r *antColonyRouter) RecordFailure(nodeID int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cur, ok := r.pheromones[nodeID]
	if !ok {
		cur = InitialPheromone
	}

	r.failures++
	r.attempts++
	r.observePenaltyLocked()
	cur *= r.penalty
	if cur < MinPheromone {
		cur = MinPheromone
	}
	r.pheromones[nodeID] = cur
}

// Evaporate decays all trails toward MinPheromone. Must be called with lock held or via public Evaporate().
func (r *antColonyRouter) Evaporate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evaporateLocked()
}

func (r *antColonyRouter) evaporateLocked() {
	for id, p := range r.pheromones {
		decayed := p * r.evaporation
		if decayed < MinPheromone {
			decayed = MinPheromone
		}
		r.pheromones[id] = decayed
	}
	r.lastEvaporate = time.Now()
}

// observeRTTLocked folds one successful RTT into the running statistics and
// re-derives the evaporation factor. Caller holds the write lock.
func (r *antColonyRouter) observeRTTLocked(rtt time.Duration) {
	if rtt <= 0 {
		return
	}
	ms := float64(rtt.Microseconds()) / 1000
	const alpha = 0.2
	if r.rttSamples == 0 {
		r.rttMean = ms
		r.rttVariance = 0
	} else {
		delta := ms - r.rttMean
		r.rttMean += alpha * delta
		r.rttVariance = (1 - alpha) * (r.rttVariance + alpha*delta*delta)
	}
	if r.rttSamples < int(^uint(0)>>1) {
		r.rttSamples++
	}
	if r.rttSamples < tuningMinSamples || r.rttMean <= 0 {
		return
	}
	// Coefficient of variation: 0 = perfectly stable, ≥1 = highly volatile.
	cv := math.Sqrt(r.rttVariance) / r.rttMean
	// Stable trails persist (ceiling), volatile ones evaporate faster (floor).
	learned := EvaporationCeiling - (EvaporationCeiling-EvaporationFloor)*math.Min(cv, 1)
	r.evaporation = clampFloat(learned, EvaporationFloor, EvaporationCeiling)
}

// observePenaltyLocked re-derives the failure penalty from the observed failure
// ratio. A high failure rate earns a harsher penalty. Caller holds the lock.
func (r *antColonyRouter) observePenaltyLocked() {
	if r.attempts == 0 {
		return
	}
	failureRate := float64(r.failures) / float64(r.attempts)
	// 0 failures → ceiling (mild), 100% failures → floor (harsh).
	learned := PenaltyCeiling - (PenaltyCeiling-PenaltyFloor)*math.Min(failureRate*2, 1)
	r.penalty = clampFloat(learned, PenaltyFloor, PenaltyCeiling)
}

// clampFloat bounds v to [lo, hi].
func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Tuning exposes the pheromone constants in force, for observability and tests.
func (r *antColonyRouter) Tuning() (evaporation, penalty float64, rttSamples int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.evaporation, r.penalty, r.rttSamples
}

// GetPheromone returns the current reputation score for a node.
func (r *antColonyRouter) GetPheromone(nodeID int) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.pheromones[nodeID]; ok {
		return p
	}
	return InitialPheromone
}

// SelectReplica chooses a candidate using weighted roulette-wheel selection.
func (r *antColonyRouter) SelectReplica(replicas []*common.Node) (selected *common.Node, err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics in replica selection (Rule 37).
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("CRITICAL: Panic recovered in PheromoneRouter.SelectReplica: %v", rec)
			err = fmt.Errorf("panic in SelectReplica: %v: %w", rec, syscall.EIO)
		}
	}()

	if len(replicas) == 0 {
		return nil, fmt.Errorf("no replica nodes provided: %w", syscall.EINVAL)
	}
	if len(replicas) == 1 {
		return replicas[0], nil
	}

	r.mu.Lock()
	if time.Since(r.lastEvaporate) >= EvaporationInterval {
		r.evaporateLocked()
	}
	total := r.sumPheromonesLocked(replicas)
	r.mu.Unlock()

	if total <= 0 {
		return replicas[0], nil
	}

	pick := rand.Float64() * total

	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sampleReplicaLocked(replicas, pick), nil
}

func (r *antColonyRouter) sumPheromonesLocked(replicas []*common.Node) float64 {
	total := 0.0
	for _, n := range replicas {
		if n == nil {
			continue
		}
		p, ok := r.pheromones[n.ID]
		if !ok {
			p = InitialPheromone
			r.pheromones[n.ID] = p
		}
		total += p
	}
	return total
}

func (r *antColonyRouter) sampleReplicaLocked(replicas []*common.Node, pick float64) *common.Node {
	running := 0.0
	for _, n := range replicas {
		if n == nil {
			continue
		}
		p, ok := r.pheromones[n.ID]
		if !ok {
			p = InitialPheromone
		}
		running += p
		if pick <= running {
			return n
		}
	}
	return replicas[0]
}

// PrioritizeReplicas orders the candidates from highest weighted priority to lowest,
// enabling sequential fallback across replicas.
func (r *antColonyRouter) PrioritizeReplicas(replicas []*common.Node) (ordered []*common.Node, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("CRITICAL: Panic recovered in PheromoneRouter.PrioritizeReplicas: %v", rec)
			err = fmt.Errorf("panic in PrioritizeReplicas: %v: %w", rec, syscall.EIO)
		}
	}()

	if len(replicas) == 0 {
		return nil, fmt.Errorf("no replica nodes provided: %w", syscall.EINVAL)
	}
	if len(replicas) == 1 {
		return []*common.Node{replicas[0]}, nil
	}

	// Make a defensive copy to prioritize without mutating caller's slice
	remaining := make([]*common.Node, 0, len(replicas))
	for _, n := range replicas {
		if n != nil {
			remaining = append(remaining, n)
		}
	}

	if len(remaining) == 0 {
		return nil, fmt.Errorf("no valid non-nil replica nodes: %w", syscall.EINVAL)
	}

	ordered = make([]*common.Node, 0, len(remaining))

	// Successive weighted sampling without replacement
	for len(remaining) > 0 {
		chosen, err := r.SelectReplica(remaining)
		if err != nil {
			// Fallback: append rest in given order
			ordered = append(ordered, remaining...)
			break
		}
		ordered = append(ordered, chosen)

		// Remove chosen from remaining
		for i, n := range remaining {
			if n.ID == chosen.ID {
				remaining = append(remaining[:i], remaining[i+1:]...)
				break
			}
		}
	}

	return ordered, nil
}
