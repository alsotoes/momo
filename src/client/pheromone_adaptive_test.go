package client

import (
	"testing"
	"time"
)

// TestPheromoneTuningDefaultsBeforeSamples keeps the legacy constants in force
// until enough RTT observations exist (Phase 3, #1145).
func TestPheromoneTuningDefaultsBeforeSamples(t *testing.T) {
	r := NewPheromoneRouter().(*antColonyRouter)
	r.RecordSuccess(1, 50*time.Millisecond)
	evap, penalty, samples := r.Tuning()
	if evap != EvaporationFactor {
		t.Fatalf("evaporation = %v before samples, want the constant %v", evap, EvaporationFactor)
	}
	if penalty != FailurePenaltyFactor {
		t.Fatalf("penalty = %v, want the constant %v", penalty, FailurePenaltyFactor)
	}
	if samples != 1 {
		t.Fatalf("samples = %d", samples)
	}
}

func TestPheromoneTuningStableNetworkKeepsTrailsLonger(t *testing.T) {
	r := NewPheromoneRouter().(*antColonyRouter)
	for i := 0; i < tuningMinSamples*2; i++ {
		r.RecordSuccess(1, 40*time.Millisecond) // perfectly stable
	}
	evap, _, samples := r.Tuning()
	if samples < tuningMinSamples {
		t.Fatalf("samples = %d", samples)
	}
	if evap < EvaporationCeiling-0.005 {
		t.Fatalf("a stable network must evaporate slowly, got %v", evap)
	}
	if evap > EvaporationCeiling {
		t.Fatalf("evaporation %v exceeds the ceiling", evap)
	}
}

func TestPheromoneTuningVolatileNetworkEvaporatesFaster(t *testing.T) {
	r := NewPheromoneRouter().(*antColonyRouter)
	for i := 0; i < tuningMinSamples; i++ {
		// Alternate 5ms and 500ms: coefficient of variation near 1.
		if i%2 == 0 {
			r.RecordSuccess(1, 5*time.Millisecond)
		} else {
			r.RecordSuccess(1, 500*time.Millisecond)
		}
	}
	evap, _, _ := r.Tuning()
	if evap >= EvaporationCeiling {
		t.Fatalf("a volatile network must evaporate faster than the ceiling, got %v", evap)
	}
	if evap < EvaporationFloor {
		t.Fatalf("evaporation %v below the floor", evap)
	}
}

func TestPheromoneTuningFailureRateHardensPenalty(t *testing.T) {
	r := NewPheromoneRouter().(*antColonyRouter)
	// One success, then a burst of failures: ratio ~0.9 → near-harsh penalty.
	r.RecordSuccess(1, 20*time.Millisecond)
	for i := 0; i < 9; i++ {
		r.RecordFailure(1)
	}
	_, penalty, _ := r.Tuning()
	if penalty >= FailurePenaltyFactor {
		t.Fatalf("a high failure rate must harden the penalty below the default %v, got %v", FailurePenaltyFactor, penalty)
	}
	if penalty < PenaltyFloor {
		t.Fatalf("penalty %v below the floor", penalty)
	}
}

func TestPheromoneTuningPenaltyRecoversWithSuccesses(t *testing.T) {
	r := NewPheromoneRouter().(*antColonyRouter)
	// Penalty only re-derives on failure, so a mix of outcomes must stay bounded
	// and never drift outside the clamp even after many attempts.
	for i := 0; i < 50; i++ {
		if i%5 == 0 {
			r.RecordFailure(1)
		} else {
			r.RecordSuccess(1, 30*time.Millisecond)
		}
	}
	_, penalty, _ := r.Tuning()
	if penalty < PenaltyFloor || penalty > PenaltyCeiling {
		t.Fatalf("penalty %v out of bounds", penalty)
	}
}

func TestPheromoneTuningAffectsEvaporation(t *testing.T) {
	r := NewPheromoneRouter().(*antColonyRouter)
	r.pheromones[1] = 10
	// Drive the router volatile so evaporation is learned below the constant.
	for i := 0; i < tuningMinSamples; i++ {
		r.RecordSuccess(1, time.Duration(5+i*40)*time.Millisecond)
	}
	r.pheromones[1] = 10
	evap, _, _ := r.Tuning()
	r.Evaporate()
	got := r.GetPheromone(1)
	want := clampFloat(10*evap, MinPheromone, MaxPheromone)
	if got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("evaporated to %v, want %v (factor %v)", got, want, evap)
	}
}

func TestClampFloat(t *testing.T) {
	if clampFloat(-1, 0, 1) != 0 || clampFloat(2, 0, 1) != 1 || clampFloat(0.5, 0, 1) != 0.5 {
		t.Fatal("clampFloat bounds violated")
	}
}
