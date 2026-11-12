package replication_order

import (
	"testing"

	"github.com/alsotoes/momo/src/common"
)

func TestScoresWithoutSnapshot(t *testing.T) {
	SetExportSnapshot(nil)
	if _, ok := Scores(); ok {
		t.Fatal("expected ok=false when no tracker is published")
	}
}

func TestScoresReflectTracker(t *testing.T) {
	tr := NewTracker([]int{common.ReplicationChain, common.ReplicationSplay}, 0.5, 1, DefaultWeights)
	tr.Observe(common.ReplicationChain, ModeResult{Success: true})
	r := NewReorderer(tr, 0)
	SetExportSnapshot(r)
	t.Cleanup(func() { SetExportSnapshot(nil) })

	scores, ok := Scores()
	if !ok {
		t.Fatal("expected ok=true with a published tracker")
	}
	if len(scores) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(scores))
	}
	// Entries follow the configured mode order.
	if scores[0].Mode != common.ReplicationChain || scores[1].Mode != common.ReplicationSplay {
		t.Fatalf("unexpected mode order: %+v", scores)
	}
	if scores[0].Score <= 0 || scores[0].Samples != 1 {
		t.Fatalf("expected Chain to have a positive score and one sample: %+v", scores[0])
	}
	if scores[1].Samples != 0 || scores[1].Score != 0 {
		t.Fatalf("expected Splay to be unobserved: %+v", scores[1])
	}
}
