package replication_order

import (
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

func TestReorderer_StabilityWindow(t *testing.T) {
	tr := NewTracker([]int{common.ReplicationChain, common.ReplicationSplay}, 0.5, 1, DefaultWeights)
	r := NewReorderer(tr, time.Hour) // huge window: never recomputes during test

	// Chain first initially.
	first := r.Order()
	if len(first) != 2 || first[0] != common.ReplicationChain {
		t.Fatalf("expected Chain first, got %v", first)
	}

	// Make Splay far better than Chain.
	tr.Observe(common.ReplicationSplay, ModeResult{Success: true, LatencyNs: 1, Bytes: 1 << 30})
	tr.Observe(common.ReplicationChain, ModeResult{Success: false, LatencyNs: 100000, Bytes: 0})

	// Within the stability window, order must not change.
	second := r.Order()
	if second[0] != common.ReplicationChain {
		t.Fatalf("stability window violated: order changed to %v", second)
	}
}

func TestReorderer_RecomputesAfterWindow(t *testing.T) {
	tr := NewTracker([]int{common.ReplicationChain, common.ReplicationSplay}, 0.5, 1, DefaultWeights)
	r := NewReorderer(tr, 0) // zero interval: recomputes every call

	r.Observe(common.ReplicationSplay, ModeResult{Success: true, LatencyNs: 1, Bytes: 1 << 30})
	r.Observe(common.ReplicationChain, ModeResult{Success: false, LatencyNs: 100000, Bytes: 0})

	order := r.Order()
	if order[0] != common.ReplicationSplay {
		t.Fatalf("expected Splay first after window elapsed, got %v", order)
	}
}

func TestReorderer_PreservesConstraints(t *testing.T) {
	tr := NewTracker([]int{common.ReplicationNone, common.ReplicationChain}, 0.5, 1, DefaultWeights)
	r := NewReorderer(tr, 0)

	// None scores highest but must stay after Chain.
	tr.Observe(common.ReplicationNone, ModeResult{Success: true, LatencyNs: 1, Bytes: 1 << 30})
	tr.Observe(common.ReplicationChain, ModeResult{Success: true, LatencyNs: 100000, Bytes: 0})

	order := r.Order()
	if order[0] != common.ReplicationChain || order[1] != common.ReplicationNone {
		t.Fatalf("expected Chain then None, got %v", order)
	}
}

func TestReorderer_Defaults(t *testing.T) {
	tr := NewTracker([]int{common.ReplicationChain}, 0.5, 1, DefaultWeights)
	r := NewReorderer(tr, 0) // explicitly zero
	if r.Order()[0] != common.ReplicationChain {
		t.Fatal("expected Chain only")
	}
}
