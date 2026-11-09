package server

import "testing"

// newTestPool returns an isolated pool so tests never touch a shared one.
func newTestPool() *replicationPool { return newReplicationPool() }

func TestPayloadPoolKeepsFloorForSmallPayloads(t *testing.T) {
	pool := newTestPool()
	for i := 0; i < payloadPoolSamplesPerResize*2; i++ {
		pool.recordSize(16)
	}
	if got := pool.targetCapacity(); got != payloadPoolMinCapacity {
		t.Fatalf("small payloads must keep the floor, got %d", got)
	}
}

func TestPayloadPoolGrowsForLargePayloads(t *testing.T) {
	pool := newTestPool()
	for i := 0; i < payloadPoolSamplesPerResize*4; i++ {
		pool.recordSize(64 * 1024)
	}
	got := pool.targetCapacity()
	if got <= payloadPoolMinCapacity {
		t.Fatalf("large payloads must grow the pool, got %d", got)
	}
	if got > payloadPoolMaxCapacity {
		t.Fatalf("pool exceeded the ceiling: %d", got)
	}
}

func TestPayloadPoolClampsOversizedPayloads(t *testing.T) {
	pool := newTestPool()
	for i := 0; i < payloadPoolSamplesPerResize*4; i++ {
		pool.recordSize(8 << 20) // 8 MiB: above the 1 MiB ceiling
	}
	if got := pool.targetCapacity(); got != payloadPoolMaxCapacity {
		t.Fatalf("target = %d, want ceiling %d", got, payloadPoolMaxCapacity)
	}
}

func TestPayloadPoolResizesOnlyAfterSampleThreshold(t *testing.T) {
	pool := newTestPool()
	// One short of the threshold: no resize yet.
	for i := 0; i < payloadPoolSamplesPerResize-1; i++ {
		pool.recordSize(64 * 1024)
	}
	if got := pool.targetCapacity(); got != payloadPoolMinCapacity {
		t.Fatalf("resized before enough samples: %d", got)
	}
	pool.recordSize(64 * 1024)
	if got := pool.targetCapacity(); got <= payloadPoolMinCapacity {
		t.Fatalf("did not resize at the threshold: %d", got)
	}
}

func TestRecordPayloadSizeIgnoresNonPositive(t *testing.T) {
	pool := newTestPool()
	pool.recordSize(0)
	pool.recordSize(-5)
	if mean, samples := pool.learner.meanAndSamples(); samples != 0 || mean != 0 {
		t.Fatalf("invalid sizes must be ignored: mean=%v samples=%d", mean, samples)
	}
}

func TestClampCapacity(t *testing.T) {
	if clampCapacity(1) != payloadPoolMinCapacity {
		t.Fatal("floor not applied")
	}
	if clampCapacity(payloadPoolMaxCapacity*2) != payloadPoolMaxCapacity {
		t.Fatal("ceiling not applied")
	}
	if clampCapacity(4096) != 4096 {
		t.Fatal("in-range value changed")
	}
}

// TestReplicationPoolsAreIndependent proves the pool is per instance: resizing
// one server's pool must not change another's target (the process-global pool
// this replaced shared one adaptive target across every daemon).
func TestReplicationPoolsAreIndependent(t *testing.T) {
	a, b := newTestPool(), newTestPool()
	for i := 0; i < payloadPoolSamplesPerResize*4; i++ {
		a.recordSize(64 * 1024)
	}
	if a.targetCapacity() <= payloadPoolMinCapacity {
		t.Fatal("pool A did not grow")
	}
	if b.targetCapacity() != payloadPoolMinCapacity {
		t.Fatalf("pool B changed to %d; pools must not share state", b.targetCapacity())
	}
}
