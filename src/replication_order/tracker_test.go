package replication_order

import (
	"testing"

	"github.com/alsotoes/momo/src/common"
)

func TestTracker_OrderByScore(t *testing.T) {
	tr := NewTracker([]int{common.ReplicationNone, common.ReplicationChain}, 0.5, 1, DefaultWeights)

	// Chain succeeds every time; None fails every time.
	tr.Observe(common.ReplicationChain, ModeResult{Success: true, LatencyNs: 1000, Bytes: 64 << 10})
	tr.Observe(common.ReplicationNone, ModeResult{Success: false, LatencyNs: 100, Bytes: 0})

	order := tr.Order()
	if order[0] != common.ReplicationChain {
		t.Fatalf("expected Chain first, got %v", order)
	}
}

func TestTracker_NeverPutsNoneBeforeReplicated(t *testing.T) {
	tr := NewTracker([]int{common.ReplicationNone, common.ReplicationChain, common.ReplicationSplay}, 0.5, 1, DefaultWeights)

	// None scores highest (contrived), but constraints must keep it after replicated modes.
	tr.Observe(common.ReplicationNone, ModeResult{Success: true, LatencyNs: 1, Bytes: 1 << 30})
	tr.Observe(common.ReplicationChain, ModeResult{Success: true, LatencyNs: 100000, Bytes: 0})
	tr.Observe(common.ReplicationSplay, ModeResult{Success: true, LatencyNs: 100000, Bytes: 0})

	order := tr.Order()
	noneIdx := -1
	lastReplicated := -1
	for i, m := range order {
		switch m {
		case common.ReplicationNone:
			noneIdx = i
		case common.ReplicationChain, common.ReplicationSplay:
			lastReplicated = i
		}
	}
	if noneIdx <= lastReplicated {
		t.Fatalf("None (%d) must not precede replicated modes (%d): %v", noneIdx, lastReplicated, order)
	}
}

func TestTracker_UnobservedModesKeepConfigOrder(t *testing.T) {
	tr := NewTracker([]int{common.ReplicationSplay, common.ReplicationChain, common.ReplicationNone}, 0.5, 100, DefaultWeights)
	order := tr.Order()
	if len(order) != 3 {
		t.Fatalf("expected 3 modes, got %d: %v", len(order), order)
	}
	// minSamples=100 > Samples=0, so no re-ranking; config order preserved.
	for i, m := range []int{common.ReplicationSplay, common.ReplicationChain, common.ReplicationNone} {
		if order[i] != m {
			t.Fatalf("expected config order preserved, got %v", order)
		}
	}
}

func TestTracker_ScoreAndStats(t *testing.T) {
	tr := NewTracker([]int{common.ReplicationChain}, 0.5, 1, DefaultWeights)
	tr.Observe(common.ReplicationChain, ModeResult{Success: true, LatencyNs: 1000, Bytes: 1024})

	s := tr.Stats(common.ReplicationChain)
	if s == nil {
		t.Fatal("expected stats")
	}
	if s.Samples != 1 {
		t.Fatalf("expected 1 sample, got %d", s.Samples)
	}
	if s.Score <= 0 {
		t.Fatalf("expected positive score, got %f", s.Score)
	}
	if tr.Score(common.ReplicationNone) != 0 {
		t.Fatal("unobserved mode should have score 0")
	}
}

func TestTracker_EWMADecaysOldSamples(t *testing.T) {
	tr := NewTracker([]int{common.ReplicationChain}, 1.0, 1, DefaultWeights) // alpha=1: full weight on latest
	tr.Observe(common.ReplicationChain, ModeResult{Success: true})
	first := tr.Score(common.ReplicationChain)

	tr.Observe(common.ReplicationChain, ModeResult{Success: false})
	second := tr.Score(common.ReplicationChain)

	if second >= first {
		t.Fatalf("expected score to drop after a failure, first=%f second=%f", first, second)
	}
}

func TestDefaultWeightsSumToOne(t *testing.T) {
	w := DefaultWeights
	if d := w.Success + w.Latency + w.Throughput + w.Error - 1.0; d < -1e-9 || d > 1e-9 {
		t.Fatalf("weights must sum to 1, got %v", w)
	}
}

// keep the linter happy about time import in tests that don't use it directly.
