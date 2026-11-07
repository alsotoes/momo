package server

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

type recordingPusher struct {
	pushed []string
	fail   bool
}

func (r *recordingPusher) Push(hash string, _ int) error {
	if r.fail {
		return fmt.Errorf("unreachable peer")
	}
	r.pushed = append(r.pushed, hash)
	return nil
}

func rebalanceTestStore(t *testing.T, n int) (*storage.CASStore, []string) {
	t.Helper()
	s, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	var hashes []string
	for i := 0; i < n; i++ {
		payload := []byte(strings.Repeat("p", 16) + string(rune('a'+i)))
		hash := common.HashBytes(payload)
		hashes = append(hashes, hash)
		if err := s.Put(fmt.Sprintf("obj-%d", i), hash, int64(len(payload)), "", bytes.NewReader(payload)); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	return s, hashes
}

func alwaysShedConfig(max int) common.ConfigurationRebalance {
	// high water 0 forces a shed on every cycle; low water 0 stops only when
	// the cycle cap or the blob list runs out.
	return common.ConfigurationRebalance{Enabled: true, Interval: 60, DiskHighWater: 0.0001, DiskLowWater: 0, MaxShedPerCycle: max}
}

func testCmap(t *testing.T) *common.ClusterMap {
	t.Helper()
	return &common.ClusterMap{Nodes: []*common.Node{
		{ID: 0, Weight: 1},
		{ID: 1, Weight: 1, Domain: "b"},
	}}
}

func TestRebalanceCycleShedsColdBlobs(t *testing.T) {
	store, _ := rebalanceTestStore(t, 3)
	pusher := &recordingPusher{}
	moved, err := rebalanceCycle(store, pusher, testCmap(t), 0, 2, alwaysShedConfig(2))
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if moved != 2 || len(pusher.pushed) != 2 {
		t.Fatalf("expected 2 moves, got moved=%d pushed=%d", moved, len(pusher.pushed))
	}
	remaining, err := store.ListBlobs()
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 blob left, got %d", len(remaining))
	}
}

func TestRebalanceCycleBelowHighWaterNoOp(t *testing.T) {
	store, _ := rebalanceTestStore(t, 2)
	pusher := &recordingPusher{}
	cfg := alwaysShedConfig(2)
	cfg.DiskHighWater, cfg.DiskLowWater = 100, 99
	moved, err := rebalanceCycle(store, pusher, testCmap(t), 0, 2, cfg)
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if moved != 0 || len(pusher.pushed) != 0 {
		t.Fatalf("nothing should move, got %d", moved)
	}
}

func TestRebalanceCycleKeepsBlobWhenPushFails(t *testing.T) {
	store, _ := rebalanceTestStore(t, 2)
	pusher := &recordingPusher{fail: true}
	moved, err := rebalanceCycle(store, pusher, testCmap(t), 0, 2, alwaysShedConfig(2))
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if moved != 0 {
		t.Fatalf("failed pushes must not count as moves")
	}
	blobs, _ := store.ListBlobs()
	if len(blobs) != 2 {
		t.Fatalf("a failed push must keep the local bytes, got %d blobs", len(blobs))
	}
}

func TestRebalanceCycleNoPeerKeepsBlob(t *testing.T) {
	store, _ := rebalanceTestStore(t, 1)
	pusher := &recordingPusher{}
	// Single-node placement: no shed target.
	cmap := &common.ClusterMap{Nodes: []*common.Node{{ID: 0, Weight: 1}}}
	moved, err := rebalanceCycle(store, pusher, cmap, 0, 1, alwaysShedConfig(4))
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if moved != 0 {
		t.Fatalf("no peer means no shedding, got %d", moved)
	}
	blobs, _ := store.ListBlobs()
	if len(blobs) != 1 {
		t.Fatalf("blob must stay, got %d", len(blobs))
	}
}

func TestStartRebalanceLoop_Disabled(t *testing.T) {
	store, _ := rebalanceTestStore(t, 1)
	startRebalanceLoop(t.Context(), store, &recordingPusher{}, testCmap(t), 0, 2, common.ConfigurationRebalance{Enabled: false})
}

func TestPickShedTarget(t *testing.T) {
	cmap := testCmap(t)
	target, ok := pickShedTarget(cmap, "some-hash", 0, 2)
	if !ok || target != 1 {
		t.Fatalf("expected peer 1, got %d ok=%v", target, ok)
	}
	if _, ok := pickShedTarget(cmap, "some-hash", 0, 1); ok {
		t.Fatal("rf=1 has no peer to shed to")
	}
}
