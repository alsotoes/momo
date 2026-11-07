package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

type recordingPusher struct {
	mu     sync.Mutex
	pushed []string
	fail   bool
}

func (r *recordingPusher) Push(hash string, _ int) error {
	if r.fail {
		return fmt.Errorf("unreachable peer")
	}
	r.mu.Lock()
	r.pushed = append(r.pushed, hash)
	r.mu.Unlock()
	return nil
}

func (r *recordingPusher) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pushed)
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

func TestRunRebalanceTicksAndStops(t *testing.T) {
	store, _ := rebalanceTestStore(t, 2)
	pusher := &recordingPusher{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r := &rebalanceRunner{store: store, pusher: pusher, cmap: testCmap(t), self: 0, rf: 2, cfg: alwaysShedConfig(10)}
		r.run(ctx, 5*time.Millisecond)
		close(done)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for pusher.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not stop on context cancel")
	}
	if pusher.count() == 0 {
		t.Fatal("expected at least one shed")
	}
}

func TestStartRebalanceLoop_EnabledLogsAndRuns(t *testing.T) {
	store, _ := rebalanceTestStore(t, 1)
	startRebalanceLoop(t.Context(), store, &recordingPusher{}, testCmap(t), 0, 1, common.ConfigurationRebalance{
		Enabled: true, Interval: 3600, DiskHighWater: 99, DiskLowWater: 90, MaxShedPerCycle: 1,
	})
}

func TestPickShedTargetInvalidMap(t *testing.T) {
	if _, ok := pickShedTarget(&common.ClusterMap{}, "hash", 0, 2); ok {
		t.Fatal("empty cluster map must yield no target")
	}
}

func TestPushWithoutStore(t *testing.T) {
	src := &DaemonRebuildSource{}
	if err := src.Push("hash", 1); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("want EINVAL, got %v", err)
	}
}

func TestPushReadsLocalBlobAndFailsCleanlyWhenMissing(t *testing.T) {
	store, _ := rebalanceTestStore(t, 1)
	src := &DaemonRebuildSource{}
	src.SetStore(store)
	// Unknown hash: read fails before any transport work.
	if err := src.Push("does-not-exist", 1); err == nil {
		t.Fatal("expected read error for a missing blob")
	}
}

func TestPushSuccessPath(t *testing.T) {
	store, hashes := rebalanceTestStore(t, 1)
	src := &DaemonRebuildSource{}
	src.SetStore(store)
	var gotHash string
	var gotSize int64
	src.pushFn = func(_ int, hash string, data []byte, meta *common.FileMetadata) error {
		gotHash, gotSize = hash, meta.Size
		if int64(len(data)) != meta.Size {
			t.Errorf("payload/meta size mismatch")
		}
		return nil
	}
	if err := src.Push(hashes[0], 1); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if gotHash != hashes[0] || gotSize == 0 {
		t.Fatalf("unexpected push args hash=%s size=%d", gotHash, gotSize)
	}
}
