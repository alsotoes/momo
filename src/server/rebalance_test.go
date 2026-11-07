package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/p2p"
	"github.com/alsotoes/momo/src/storage"
)

type recordingPusher struct {
	mu     sync.Mutex
	pushed []string
	fail   bool
}

func (r *recordingPusher) Push(_ context.Context, hash string, _ int) error {
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

func testCmap3(t *testing.T) *common.ClusterMap {
	t.Helper()
	return &common.ClusterMap{Nodes: []*common.Node{
		{ID: 0, Weight: 1},
		{ID: 1, Weight: 1, Domain: "b"},
		{ID: 2, Weight: 1, Domain: "c"},
	}}
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
	moved, err := rebalanceCycle(context.Background(), store, pusher, testCmap(t), 0, 2, alwaysShedConfig(2))
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
	moved, err := rebalanceCycle(context.Background(), store, pusher, testCmap(t), 0, 2, cfg)
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
	moved, err := rebalanceCycle(context.Background(), store, pusher, testCmap(t), 0, 2, alwaysShedConfig(2))
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
	moved, err := rebalanceCycle(context.Background(), store, pusher, cmap, 0, 1, alwaysShedConfig(4))
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
	if err := src.Push(context.Background(), "hash", 1); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("want EINVAL, got %v", err)
	}
}

func TestPushReadsLocalBlobAndFailsCleanlyWhenMissing(t *testing.T) {
	store, _ := rebalanceTestStore(t, 1)
	src := &DaemonRebuildSource{}
	src.SetStore(store)
	// Unknown hash: read fails before any transport work.
	if err := src.Push(context.Background(), "does-not-exist", 1); err == nil {
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
	if err := src.Push(context.Background(), hashes[0], 1); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if gotHash != hashes[0] || gotSize == 0 {
		t.Fatalf("unexpected push args hash=%s size=%d", gotHash, gotSize)
	}
}

func findPlacementHash(t *testing.T, cmap *common.ClusterMap, rf int, self, peer int, wantSelf bool) (string, []byte) {
	t.Helper()
	for i := 0; i < 10000; i++ {
		payload := []byte(fmt.Sprintf("placement-probe-%d", i))
		hash := common.HashBytes(payload)
		placement, err := cmap.Placement(hash, rf)
		if err != nil {
			t.Fatalf("placement: %v", err)
		}
		var hasSelf, hasPeer bool
		for _, n := range placement {
			hasSelf = hasSelf || n.ID == self
			hasPeer = hasPeer || n.ID == peer
		}
		if hasSelf == wantSelf && hasPeer {
			return hash, payload
		}
	}
	t.Fatalf("no hash found with self=%v peer=%d", wantSelf, peer)
	return "", nil
}

func putBlob(t *testing.T, store *storage.CASStore, name, hash string, payload []byte) {
	t.Helper()
	if err := store.Put(name, hash, int64(len(payload)), "", bytes.NewReader(payload)); err != nil {
		t.Fatalf("put %s: %v", name, err)
	}
}

func joinRunner(t *testing.T, store *storage.CASStore, pusher rebalancePusher, cmap *common.ClusterMap, self, rf, max int) *rebalanceRunner {
	t.Helper()
	return &rebalanceRunner{store: store, pusher: pusher, cmap: cmap, self: self, rf: rf, cfg: common.ConfigurationRebalance{Enabled: true, Interval: 60, DiskHighWater: 85, DiskLowWater: 70, MaxShedPerCycle: max}}
}

func TestMigrateToPeerMovesOwnedBlob(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cmap := testCmap3(t)
	hash, payload := findPlacementHash(t, cmap, 2, 0, 1, false)
	putBlob(t, store, "moved", hash, payload)

	pusher := &recordingPusher{}
	r := joinRunner(t, store, pusher, cmap, 0, 2, 8)
	moved, err := r.migrateToPeer(context.Background(), 1)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if moved != 1 || pusher.count() != 1 {
		t.Fatalf("expected 1 move, got moved=%d pushed=%d", moved, pusher.count())
	}
	if blobs, _ := store.ListBlobs(); len(blobs) != 0 {
		t.Fatalf("local bytes must be dropped after ack, got %d", len(blobs))
	}
}

func TestMigrateToPeerKeepsBlobThisNodeStillOwns(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cmap := testCmap3(t)
	hash, payload := findPlacementHash(t, cmap, 2, 0, 1, true)
	putBlob(t, store, "kept", hash, payload)

	r := joinRunner(t, store, &recordingPusher{}, cmap, 0, 2, 8)
	moved, err := r.migrateToPeer(context.Background(), 1)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if moved != 0 {
		t.Fatalf("a blob this node still owns must not move")
	}
	if blobs, _ := store.ListBlobs(); len(blobs) != 1 {
		t.Fatalf("blob must stay local, got %d", len(blobs))
	}
}

func TestMigrateToPeerPushFailureKeepsBytes(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cmap := testCmap3(t)
	hash, payload := findPlacementHash(t, cmap, 2, 0, 1, false)
	putBlob(t, store, "kept", hash, payload)

	r := joinRunner(t, store, &recordingPusher{fail: true}, cmap, 0, 2, 8)
	moved, err := r.migrateToPeer(context.Background(), 1)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if moved != 0 {
		t.Fatal("failed push must not count as a move")
	}
	if blobs, _ := store.ListBlobs(); len(blobs) != 1 {
		t.Fatalf("failed push must keep local bytes, got %d", len(blobs))
	}
}

func putMovableBlobs(t *testing.T, store *storage.CASStore, cmap *common.ClusterMap, self, peer, n int) {
	t.Helper()
	made := 0
	for i := 0; made < n && i < 100000; i++ {
		payload := []byte(fmt.Sprintf("cap-probe-%d", i))
		hash := common.HashBytes(payload)
		peerOwns, selfOwns, err := placementOwnership(cmap, hash, 2, self, peer)
		if err != nil {
			t.Fatalf("placement: %v", err)
		}
		if peerOwns && !selfOwns {
			putBlob(t, store, fmt.Sprintf("cap-%d", made), hash, payload)
			made++
		}
	}
	if made != n {
		t.Fatalf("setup: only %d blobs", made)
	}
}

func TestMigrateToPeerRespectsCap(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cmap := testCmap3(t)
	putMovableBlobs(t, store, cmap, 0, 1, 3)

	r := joinRunner(t, store, &recordingPusher{}, cmap, 0, 2, 2)
	moved, err := r.migrateToPeer(context.Background(), 1)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if moved != 2 {
		t.Fatalf("cap must bound a pass to 2, got %d", moved)
	}
	if blobs, _ := store.ListBlobs(); len(blobs) != 1 {
		t.Fatalf("one blob should remain, got %d", len(blobs))
	}
}

func TestOnPeerJoinLogsAndMigrates(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cmap := testCmap3(t)
	hash, payload := findPlacementHash(t, cmap, 2, 0, 1, false)
	putBlob(t, store, "joined", hash, payload)

	r := joinRunner(t, store, &recordingPusher{}, cmap, 0, 2, 8)
	r.onPeerJoin(context.Background(), 1)
	if blobs, _ := store.ListBlobs(); len(blobs) != 0 {
		t.Fatalf("onPeerJoin must migrate, %d blobs left", len(blobs))
	}
}

func TestJoinHandler(t *testing.T) {
	// Nil runner: logs only, no panic.
	joinHandler(context.Background(), nil)(&p2p.Peer{ID: 7})

	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cmap := testCmap3(t)
	hash, payload := findPlacementHash(t, cmap, 2, 0, 1, false)
	putBlob(t, store, "join", hash, payload)

	r := joinRunner(t, store, &recordingPusher{}, cmap, 0, 2, 8)
	joinHandler(context.Background(), r)(&p2p.Peer{ID: 1})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if blobs, _ := store.ListBlobs(); len(blobs) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("join handler must migrate asynchronously")
}

func TestNewJoinMigrator(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	src := &DaemonRebuildSource{}

	cfg := common.Configuration{}
	cfg.Rebalance.Enabled = true
	cfg.Storage.DegradedRead = true
	if newJoinMigrator(context.Background(), cfg, 0, store, src) == nil {
		t.Fatal("expected a migrator when enabled, degraded read on, and a source present")
	}
	if newJoinMigrator(context.Background(), cfg, 0, store, nil) != nil {
		t.Fatal("nil rebuild source must disable migration")
	}
	cfg.Rebalance.Enabled = false
	if newJoinMigrator(context.Background(), cfg, 0, store, src) != nil {
		t.Fatal("disabled rebalance must yield nil")
	}
}

func TestMigrateToPeerStoreError(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	_ = store.Close()
	r := joinRunner(t, store, &recordingPusher{}, testCmap3(t), 0, 2, 8)
	// A closed store must not panic the join path; errors are logged.
	r.onPeerJoin(context.Background(), 1)
}

func TestMigrateToPeerContextCancelled(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cmap := testCmap3(t)
	hash, payload := findPlacementHash(t, cmap, 2, 0, 1, false)
	putBlob(t, store, "cancel", hash, payload)

	r := joinRunner(t, store, &recordingPusher{}, cmap, 0, 2, 8)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	moved, err := r.migrateToPeer(ctx, 1)
	if err != nil || moved != 0 {
		t.Fatalf("cancelled context must stop the pass, moved=%d err=%v", moved, err)
	}
}

func TestDiskUsedPercentMissingDir(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewCASStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	_ = store.Close()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := diskUsedPercent(store); err == nil {
		t.Fatal("statfs on a missing dir must fail")
	}
}

func TestPlacementOwnershipError(t *testing.T) {
	if _, _, err := placementOwnership(&common.ClusterMap{}, "hash", 2, 0, 1); err == nil {
		t.Fatal("empty cluster map must error")
	}
}

func TestMigrateBlobDropFailureKeepsBytes(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based failure is bypassed by root")
	}
	dir := t.TempDir()
	store, err := storage.NewCASStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cmap := testCmap3(t)
	hash, payload := findPlacementHash(t, cmap, 2, 0, 1, false)
	putBlob(t, store, "keep", hash, payload)

	leaf := filepath.Join(dir, "blobs", hash[0:2], hash[2:4], hash[4:6])
	t.Cleanup(func() { _ = os.Chmod(leaf, 0o755) })
	if err := os.Chmod(leaf, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	r := joinRunner(t, store, &recordingPusher{}, cmap, 0, 2, 8)
	if moved, err := r.migrateToPeer(context.Background(), 1); err != nil || moved != 0 {
		t.Fatalf("drop failure must not count as a move, moved=%d err=%v", moved, err)
	}
}

func TestPushRecordsRebalanceMetrics(t *testing.T) {
	store, hashes := rebalanceTestStore(t, 1)
	src := &DaemonRebuildSource{}
	src.SetStore(store)
	src.SetMetrics(NewMetricsCollector())
	src.pushFn = func(int, string, []byte, *common.FileMetadata) error { return nil }
	if err := src.Push(context.Background(), hashes[0], 1); err != nil {
		t.Fatalf("push: %v", err)
	}
	if got := src.metrics.rebalanceMoved.Load(); got != 1 {
		t.Fatalf("moved = %d, want 1", got)
	}
	if src.metrics.rebalanceBytes.Load() == 0 {
		t.Fatal("byte counter must advance")
	}
	src.pushFn = func(int, string, []byte, *common.FileMetadata) error { return errors.New("boom") }
	if err := src.Push(context.Background(), hashes[0], 1); err == nil {
		t.Fatal("expected push error")
	}
	if got := src.metrics.rebalanceFailures.Load(); got != 1 {
		t.Fatalf("failures = %d, want 1", got)
	}
	src.IncRebalanceCycles()
	if got := src.metrics.rebalanceCycles.Load(); got != 1 {
		t.Fatalf("cycles = %d, want 1", got)
	}
}

func TestRebalanceMetricsRendered(t *testing.T) {
	m := NewMetricsCollector()
	m.IncRebalanceMoved()
	m.AddRebalanceBytes(1234)
	m.IncRebalanceCycles()
	m.IncRebalanceFailures()
	var buf bytes.Buffer
	m.writeMetrics(&buf)
	out := buf.String()
	for _, want := range []string{
		"momo_rebalance_blobs_moved_total 1",
		"momo_rebalance_bytes_moved_total 1234",
		"momo_rebalance_cycles_total 1",
		"momo_rebalance_failures_total 1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in metrics output", want)
		}
	}
}

// blockingPusher holds the first push until released, so two passes overlap.
type blockingPusher struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	n       int
}

func (b *blockingPusher) Push(_ context.Context, _ string, _ int) error {
	b.mu.Lock()
	b.n++
	b.mu.Unlock()
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return nil
}

func (b *blockingPusher) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.n
}

func TestPassesAreSerialized(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cmap := testCmap3(t)
	putMovableBlobs(t, store, cmap, 0, 1, 1)
	pusher := &blockingPusher{entered: make(chan struct{}), release: make(chan struct{})}
	r := joinRunner(t, store, pusher, cmap, 0, 2, 4)

	done := make(chan struct{})
	go func() {
		_ = r.onPeerJoin(context.Background(), 1)
		close(done)
	}()
	<-pusher.entered

	// A second pass must wait for the first rather than push the same blobs.
	tryPass := func() {
		r.runMu.Lock()
		defer r.runMu.Unlock()
	}
	second := make(chan struct{})
	go func() {
		tryPass()
		close(second)
	}()
	select {
	case <-second:
		t.Fatal("second pass acquired the lock while the first was mid-push")
	case <-time.After(30 * time.Millisecond):
	}

	close(pusher.release)
	<-done
	<-second
	if pusher.count() == 0 {
		t.Fatal("expected pushes")
	}
}

// rebalanceCycleForTest exposes one evaluation pass to the chaos test.
func (r *rebalanceRunner) rebalanceCycleForTest(ctx context.Context) (int, error) {
	return rebalanceCycle(ctx, r.store, r.pusher, r.cmap, r.self, r.rf, r.cfg)
}
