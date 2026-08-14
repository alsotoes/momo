package metrics

import (
	"testing"

	"github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

func TestEffectiveModeDurability(t *testing.T) {
	defer goleak.VerifyNone(t)

	cases := []struct {
		name               string
		mode               int
		factor             int
		cluster            int
		expectedDurability int
	}{
		{"none is single copy", common.ReplicationNone, 3, 5, 1},
		{"chain bounded by factor", common.ReplicationChain, 2, 5, 2},
		{"splay bounded by cluster", common.ReplicationSplay, 5, 2, 2},
		{"primary-splay equals factor", common.ReplicationPrimarySplay, 3, 5, 3},
		{"factor equals cluster", common.ReplicationChain, 3, 3, 3},
		{"cluster of one", common.ReplicationChain, 3, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := effectiveModeDurability(tc.mode, tc.factor, tc.cluster)
			if got != tc.expectedDurability {
				t.Fatalf("expected durability %d, got %d", tc.expectedDurability, got)
			}
		})
	}
}

func TestShouldSwitchMode(t *testing.T) {
	defer goleak.VerifyNone(t)

	// order: idx0=None(1), idx1=Chain(up to factor), idx2=Splay, idx3=PrimarySplay
	order := []int{common.ReplicationNone, common.ReplicationChain, common.ReplicationSplay, common.ReplicationPrimarySplay}
	factor := 3
	cluster := 4

	cases := []struct {
		name     string
		floor    int
		newIdx   int
		expected bool
	}{
		{"floor disabled allows none", 0, 0, true},
		{"floor 2 blocks none", 2, 0, false},
		{"floor 1 allows none", 1, 0, true},
		{"floor 2 allows chain", 2, 1, true},
		{"floor 3 allows splay", 3, 2, true},
		{"floor 3 allows chain at factor 3", 3, 1, true},
		{"floor 3 allows primary-splay", 3, 3, true},
		{"floor 4 blocks everything replicated (cap 3)", 4, 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldSwitchMode(tc.floor, order, tc.newIdx, factor, cluster)
			if got != tc.expected {
				t.Fatalf("expected shouldSwitchMode=%v, got %v", tc.expected, got)
			}
		})
	}
}

func TestShouldSwitchMode_OutOfRange(t *testing.T) {
	defer goleak.VerifyNone(t)

	order := []int{common.ReplicationNone, common.ReplicationChain}
	// Out-of-range newIdx with floor enabled: helper is conservative, returns
	// true (no meta to judge) so it cannot spuriously block.
	if !shouldSwitchMode(2, order, 99, 3, 4) {
		t.Fatal("expected out-of-range index to be allowed conservatively")
	}
}
