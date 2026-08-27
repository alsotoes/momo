package storage

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

// testCluster simulates a small replica set of real CASStore instances with a
// Rule 74 in-memory RebuildSource per member. Blob presence is the replica
// unit; verifiedness is only enforced by Fetch + the storage verify-before-use
// path (R2-C4), so a corrupt survivor is a real test vector.
type testCluster struct {
	stores  []*CASStore
	domains []string
	mu      sync.Mutex
}

func newTestCluster(t *testing.T, domains []string) *testCluster {
	t.Helper()
	stores := make([]*CASStore, len(domains))
	for i := range domains {
		tmp := t.TempDir()
		store, err := NewCASStore(tmp)
		if err != nil {
			t.Fatalf("store %d: %v", i, err)
		}
		stores[i] = store
	}
	return &testCluster{stores: stores, domains: domains}
}

func (c *testCluster) close(t *testing.T) {
	t.Helper()
	for _, s := range c.stores {
		if err := s.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}
}

// hasBlob reports whether member i holds ANY bytes for hash (presence only —
// corruption is a verified-fetch concern).
func (c *testCluster) hasBlob(i int, hash string) bool {
	rc, err := c.stores[i].blobs.GetBlob(hash)
	if err != nil {
		return false
	}
	defer rc.Close()
	return rc != nil
}

// verifiedBlob reports whether member i holds bytes that re-derive to hash.
func (c *testCluster) verifiedBlob(i int, hash string) bool {
	rc, err := c.stores[i].blobs.GetBlob(hash)
	if err != nil {
		return false
	}
	defer rc.Close()
	digest, err := common.HashReader(rc)
	if err != nil {
		return false
	}
	return digest == hash
}

// testSource is the RebuildSource for member self of a testCluster.
type testSource struct {
	c    *testCluster
	self int
}

func (ts testSource) Survivors(hash string) ([]Peer, error) {
	ts.c.mu.Lock()
	defer ts.c.mu.Unlock()
	var out []Peer
	for i := range ts.c.stores {
		if i == ts.self {
			continue
		}
		if ts.c.hasBlob(i, hash) {
			out = append(out, Peer{ID: i, Domain: ts.c.domains[i]})
		}
	}
	return out, nil
}

func (ts testSource) Fetch(hash string, survivor Peer) (io.ReadCloser, error) {
	ts.c.mu.Lock()
	defer ts.c.mu.Unlock()
	rc, err := ts.c.stores[survivor.ID].blobs.GetBlob(hash)
	if err != nil {
		return nil, err
	}
	return rc, nil
}

func (ts testSource) Restore(hash string, content io.Reader) error {
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	ts.c.mu.Lock()
	defer ts.c.mu.Unlock()
	// Restore to every member that lacks the blob. Prefer members whose
	// failure domain differs from the survivor's domain first (R2-C5, R1):
	// repair spreads across independent failure units when the cluster size
	// allows.
	missing := make([]int, 0, len(ts.c.stores))
	for i := range ts.c.stores {
		if i == ts.self {
			continue
		}
		if !ts.c.hasBlob(i, hash) {
			missing = append(missing, i)
		}
	}
	mine := ts.c.domains[ts.self]
	sort.SliceStable(missing, func(i, j int) bool {
		di, dj := ts.c.domains[missing[i]], ts.c.domains[missing[j]]
		if di != mine && dj == mine {
			return true
		}
		if di == mine && dj != mine {
			return false
		}
		return di < dj
	})
	for _, i := range missing {
		if err := ts.c.stores[i].blobs.PutBlob(hash, bytes.NewReader(data)); err != nil {
			return err
		}
	}
	return nil
}

// mustHash returns the content-address key of data.
func mustHash(t *testing.T, data []byte) string {
	t.Helper()
	return common.HashBytes(data)
}

// readAll gets name from store and returns the drained bytes.
func readAll(t *testing.T, store *CASStore, name string) ([]byte, error) {
	t.Helper()
	rc, meta, err := store.Get(name)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	if meta.Size != int64(len(data)) {
		return nil, fmt.Errorf("size mismatch: meta %d got %d", meta.Size, len(data))
	}
	return data, nil
}

// TestRebuildRestoresUnderreplicatedReplica (R2-T2) verifies the self-heal loop
// re-replicates a blob whose replica was dropped back to the target count.
func TestRebuildRestoresUnderreplicatedReplica(t *testing.T) {
	defer goleak.VerifyNone(t)

	c := newTestCluster(t, []string{"rack-a", "rack-b", "rack-c"})
	defer c.close(t)

	data := []byte("fixed content for rebuild test")
	hash := putBlob(t, c.stores[0], "obj-1", data)
	// Members 1 and 2 hold replica bytes (as a replicated cluster would).
	c.stores[1].blobs.PutBlob(hash, bytes.NewReader(data))
	c.stores[2].blobs.PutBlob(hash, bytes.NewReader(data))

	// Drop replica 2.
	if err := c.stores[2].blobs.DeleteBlob(hash); err != nil {
		t.Fatalf("drop replica: %v", err)
	}

	cfg := RebuildConfig{
		Interval: time.Hour,
		Workers:  2,
		Target:   3,
		Source:   testSource{c: c, self: 0},
	}
	if err := c.stores[0].runRebuild(cfg); err != nil {
		t.Fatalf("runRebuild: %v", err)
	}

	for i := range c.stores {
		if !c.verifiedBlob(i, hash) {
			t.Errorf("member %d does not hold verified replica after rebuild", i)
		}
	}
	if got := c.stores[0].RepairCount(); got != 1 {
		t.Errorf("RepairCount = %d, want 1", got)
	}

	// Idempotent: a healthy pass makes no further repairs.
	c.stores[0].repairs.Store(0)
	if err := c.stores[0].runRebuild(cfg); err != nil {
		t.Fatalf("second runRebuild: %v", err)
	}
	if got := c.stores[0].RepairCount(); got != 0 {
		t.Errorf("healthy pass made %d repairs, want 0", got)
	}
}

// TestRebuildRestoresQuarantinedMarkedLocal (R2-C2/R2-T2) verifies a local
// mark-and-hold copy is replaced from a verified survivor and the mark cleared.
func TestRebuildRestoresQuarantinedMarkedLocal(t *testing.T) {
	defer goleak.VerifyNone(t)

	c := newTestCluster(t, []string{"rack-a", "rack-b"})
	defer c.close(t)

	data := []byte("mark-and-hold replacement content")
	hash := putBlob(t, c.stores[0], "obj-q", data)
	c.stores[1].blobs.PutBlob(hash, bytes.NewReader(data))

	// Simulate a corrupt local copy surfaced by a verified read: mark-and-hold.
	if err := c.stores[0].quarantineMark(hash); err != nil {
		t.Fatalf("quarantineMark: %v", err)
	}

	cfg := RebuildConfig{
		Interval: time.Hour,
		Workers:  2,
		Target:   3,
		Source:   testSource{c: c, self: 0},
	}
	if err := c.stores[0].runRebuild(cfg); err != nil {
		t.Fatalf("runRebuild: %v", err)
	}

	if c.stores[0].quarantined(hash) {
		t.Error("quarantine mark not cleared after verified replacement")
	}
	if !c.verifiedBlob(0, hash) {
		t.Error("local blob not restored to verified content")
	}
	if got := c.stores[0].RepairCount(); got == 0 {
		t.Error("expected a repair to be counted")
	}
}

// TestDegradedReadServesSurvivorWhenLocalMissing (R2-T1) verifies Get degrades
// to a verified survivor when the local blob is missing, heals locally, and
// returns ENOENT when no survivor exists.
func TestDegradedReadServesSurvivorWhenLocalMissing(t *testing.T) {
	defer goleak.VerifyNone(t)

	c := newTestCluster(t, []string{"rack-a", "rack-b"})
	defer c.close(t)

	data := []byte("degraded read bytes")
	hash := mustHash(t, data)

	// Primary has object metadata but the local blob is missing (lost).
	if err := c.stores[0].Put("obj-d", hash, int64(len(data)), "", nil); err != nil {
		t.Fatalf("Put meta-only: %v", err)
	}
	// Survivor replica holds verified bytes.
	c.stores[1].blobs.PutBlob(hash, bytes.NewReader(data))

	source := c.stores[0]
	source.StartRebuild(RebuildConfig{
		Interval:     time.Hour,
		Workers:      2,
		Target:       3,
		Source:       testSource{c: c, self: 0},
		DegradedRead: true,
	})

	got, err := readAll(t, source, "obj-d")
	if err != nil {
		t.Fatalf("degraded Get failed: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("served %q, want %q", got, data)
	}
	// Repair-on-read materialized the local copy.
	if !c.verifiedBlob(0, hash) {
		t.Error("degraded read did not heal the local copy")
	}
	if c.stores[0].quarantined(hash) {
		t.Error("quarantine mark left set after successful degraded read")
	}
}

// TestDegradedReadENOENTWithoutSurvivor (R2-T1) verifies a read fails ENOENT
// only when no verified survivor exists in the placement.
func TestDegradedReadENOENTWithoutSurvivor(t *testing.T) {
	defer goleak.VerifyNone(t)

	c := newTestCluster(t, []string{"rack-a", "rack-b"})
	defer c.close(t)

	data := []byte("no survivor content")
	hash := mustHash(t, data)
	if err := c.stores[0].Put("obj-e", hash, int64(len(data)), "", nil); err != nil {
		t.Fatalf("Put meta-only: %v", err)
	}

	c.stores[0].StartRebuild(RebuildConfig{
		Interval:     time.Hour,
		Workers:      2,
		Target:       3,
		Source:       testSource{c: c, self: 0},
		DegradedRead: true,
	})

	_, err := readAll(t, c.stores[0], "obj-e")
	if err == nil {
		t.Fatal("expected ENOENT with no survivor, got nil")
	}
	if !strings.Contains(err.Error(), syscall.ENOENT.Error()) {
		t.Fatalf("expected ENOENT, got %v", err)
	}
}

// TestDegradedReadRejectsCorruptSurvivor (R2-T3/R2-C4) verifies verify-before-
// use: a survivor serving corrupt bytes is rejected, a good one is used, and
// corrupt bytes never land in the store.
func TestDegradedReadRejectsCorruptSurvivor(t *testing.T) {
	defer goleak.VerifyNone(t)

	c := newTestCluster(t, []string{"rack-a", "rack-b", "rack-c"})
	defer c.close(t)

	good := []byte("verified survivor payload")
	hash := mustHash(t, good)

	if err := c.stores[0].Put("obj-c", hash, int64(len(good)), "", nil); err != nil {
		t.Fatalf("Put meta-only: %v", err)
	}
	// Member 1 advertises presence but serves corrupt bytes.
	c.stores[1].blobs.PutBlob(hash, bytes.NewReader([]byte("corrupt bogus bytes!")))
	// Member 2 serves the real content.
	c.stores[2].blobs.PutBlob(hash, bytes.NewReader(good))

	// Force survivor ordering [1,2]: restart a source that lists by presence —
	// presence puts member 1 first (member index order), exercising the corrupt
	// rejection path before the good fetch.
	c.stores[0].StartRebuild(RebuildConfig{
		Interval:     time.Hour,
		Workers:      2,
		Target:       3,
		Source:       testSource{c: c, self: 0},
		DegradedRead: true,
	})

	got, err := readAll(t, c.stores[0], "obj-c")
	if err != nil {
		t.Fatalf("Get after corrupt-survivor rejection failed: %v", err)
	}
	if !bytes.Equal(got, good) {
		t.Errorf("served %q, want %q", got, good)
	}
	if !c.verifiedBlob(0, hash) {
		t.Error("local copy is not verified content after degraded read")
	}
}

// TestStartRebuildIdempotentAndClose (R2-T4) verifies StartRebuild is a
// one-shot (mirroring StartScrub/StartGC), Close stops the loop, and no
// goroutines leak.
func TestStartRebuildIdempotentAndClose(t *testing.T) {
	defer goleak.VerifyNone(t)

	c := newTestCluster(t, []string{"rack-a"})
	store := c.stores[0]

	cfg := RebuildConfig{
		Interval:     30 * time.Millisecond,
		Workers:      2,
		Target:       3,
		Source:       testSource{c: c, self: 0},
		DegradedRead: true,
	}
	store.StartRebuild(cfg)
	store.StartRebuild(cfg) // second call must be a no-op

	if store.rebuildStarted.Load() != 1 {
		t.Errorf("rebuildStarted = %d, want 1", store.rebuildStarted.Load())
	}
	// Give the loop at least one tick, then Close must stop it cleanly.
	time.Sleep(120 * time.Millisecond)
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestQuarantineMarkAndGetLegacy (R2-C2) verifies the R2-C2 mark-and-hold
// option: a marked blob reads as ENOENT when degraded read is not armed, and
// hard-delete quarantine behavior is unchanged.
func TestQuarantineMarkAndGetLegacy(t *testing.T) {
	defer goleak.VerifyNone(t)

	c := newTestCluster(t, []string{"rack-a"})
	defer c.close(t)

	data := []byte("marked but not deleted")
	hash := putBlob(t, c.stores[0], "obj-m", data)

	if err := c.stores[0].quarantineMark(hash); err != nil {
		t.Fatalf("quarantineMark: %v", err)
	}
	// Blob bytes remain (mark-and-hold, no hard delete).
	if !c.hasBlob(0, hash) {
		t.Error("mark-and-hold hard-deleted the blob")
	}
	// Without degraded read the read path refuses to serve the marked copy.
	c.stores[0].StartRebuild(RebuildConfig{
		Interval: time.Hour,
		Workers:  2,
		Target:   3,
		Source:   testSource{c: c, self: 0},
	})
	_, err := readAll(t, c.stores[0], "obj-m")
	if err == nil {
		t.Fatal("expected ENOENT for mark-and-hold copy")
	}
	if err := c.stores[0].quarantineClear(hash); err != nil {
		t.Fatalf("quarantineClear: %v", err)
	}
	// Legacy hard-delete quarantine (scrub path) still removes bytes.
	if err := c.stores[0].quarantine(hash); err != nil {
		t.Fatalf("legacy quarantine: %v", err)
	}
	if c.hasBlob(0, hash) {
		t.Error("legacy quarantine did not hard-delete the blob")
	}
}

// TestNewStoreWithRebuild wires the factory path: StartRebuild armed with the
// configured R2 keys, RepairCount reachable, close clean.
func TestNewStoreWithRebuild(t *testing.T) {
	defer goleak.VerifyNone(t)

	c := newTestCluster(t, []string{"rack-a", "rack-b"})
	defer c.close(t)

	cfg := common.ConfigurationStorage{
		Backend:         common.BackendLocal,
		RebuildInterval: 100,
		DegradedRead:    true,
		RebuildWorkers:  3,
	}
	daemon := &common.Daemon{Data: t.TempDir()}
	s, err := NewStoreWithRebuild(cfg, daemon, "", testSource{c: c, self: 0}, 2)
	if err != nil {
		t.Fatalf("NewStoreWithRebuild: %v", err)
	}
	cas := s.(*CASStore)
	if cas.rebuildStarted.Load() != 1 {
		t.Errorf("rebuild loop not started; started=%d", cas.rebuildStarted.Load())
	}
	if cas.rebuildTarget != 2 {
		t.Errorf("rebuildTarget = %d, want 2", cas.rebuildTarget)
	}
	if !cas.degradedRead {
		t.Error("degradedRead not armed")
	}
	if err := cas.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestRebuildSetIncludesQuarantined verifies the working set unions referenced
// blobs and quarantine-marked hashes.
func TestRebuildSetIncludesQuarantined(t *testing.T) {
	defer goleak.VerifyNone(t)

	store, err := NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer store.Close()

	data := []byte("working set content")
	hash := putBlob(t, store, "obj-w", data)
	if err := store.quarantineMark("zzz-not-referenced"); err != nil {
		t.Fatalf("quarantineMark: %v", err)
	}

	set, err := store.rebuildSet()
	if err != nil {
		t.Fatalf("rebuildSet: %v", err)
	}
	saw := map[string]bool{}
	for _, h := range set {
		saw[h] = true
	}
	if !saw[hash] {
		t.Errorf("rebuild set missing referenced blob %s: %v", hash, set)
	}
	if !saw["zzz-not-referenced"] {
		t.Errorf("rebuild set missing quarantine-marked hash: %v", set)
	}
}
