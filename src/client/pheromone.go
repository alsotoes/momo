package client

import (
	"fmt"
	"log"
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
}

// NewPheromoneRouter creates a new Ant Colony PheromoneRouter.
func NewPheromoneRouter() PheromoneRouter {
	return &antColonyRouter{
		pheromones:    make(map[int]float64),
		lastEvaporate: time.Now(),
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

	cur *= FailurePenaltyFactor
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
		decayed := p * EvaporationFactor
		if decayed < MinPheromone {
			decayed = MinPheromone
		}
		r.pheromones[id] = decayed
	}
	r.lastEvaporate = time.Now()
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
	// Trigger lazy evaporation if interval elapsed
	if time.Since(r.lastEvaporate) >= EvaporationInterval {
		r.evaporateLocked()
	}

	// Calculate total pheromone sum across candidates
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
	r.mu.Unlock()

	if total <= 0 {
		return replicas[0], nil
	}

	// Roulette wheel selection
	pick := rand.Float64() * total
	running := 0.0

	r.mu.RLock()
	defer r.mu.RUnlock()

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
			return n, nil
		}
	}

	return replicas[0], nil
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
