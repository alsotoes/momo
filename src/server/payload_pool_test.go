package server

import "testing"

// resetPoolSizing restores global pool-sizing state around a test.
func resetPoolSizing(t *testing.T) {
	t.Helper()
	origTarget := payloadPoolTarget.Load()
	origLearner := poolSizing
	poolSizing = &payloadSizeLearner{alpha: 0.1}
	t.Cleanup(func() {
		payloadPoolTarget.Store(origTarget)
		poolSizing = origLearner
	})
}

func TestPayloadPoolKeepsFloorForSmallPayloads(t *testing.T) {
	resetPoolSizing(t)
	payloadPoolTarget.Store(payloadPoolMinCapacity)
	for i := 0; i < payloadPoolSamplesPerResize*2; i++ {
		recordReplicationPayloadSize(16)
	}
	if got := payloadPoolTargetCapacity(); got != payloadPoolMinCapacity {
		t.Fatalf("small payloads must keep the floor, got %d", got)
	}
}

func TestPayloadPoolGrowsForLargePayloads(t *testing.T) {
	resetPoolSizing(t)
	payloadPoolTarget.Store(payloadPoolMinCapacity)
	for i := 0; i < payloadPoolSamplesPerResize*4; i++ {
		recordReplicationPayloadSize(64 * 1024)
	}
	got := payloadPoolTargetCapacity()
	if got <= payloadPoolMinCapacity {
		t.Fatalf("large payloads must grow the pool, got %d", got)
	}
	if got > payloadPoolMaxCapacity {
		t.Fatalf("pool exceeded the ceiling: %d", got)
	}
}

func TestPayloadPoolClampsOversizedPayloads(t *testing.T) {
	resetPoolSizing(t)
	payloadPoolTarget.Store(payloadPoolMinCapacity)
	for i := 0; i < payloadPoolSamplesPerResize*4; i++ {
		recordReplicationPayloadSize(8 << 20) // 8 MiB: above the 1 MiB ceiling
	}
	if got := payloadPoolTargetCapacity(); got != payloadPoolMaxCapacity {
		t.Fatalf("target = %d, want ceiling %d", got, payloadPoolMaxCapacity)
	}
}

func TestPayloadPoolResizesOnlyAfterSampleThreshold(t *testing.T) {
	resetPoolSizing(t)
	payloadPoolTarget.Store(payloadPoolMinCapacity)
	// One short of the threshold: no resize yet.
	for i := 0; i < payloadPoolSamplesPerResize-1; i++ {
		recordReplicationPayloadSize(64 * 1024)
	}
	if got := payloadPoolTargetCapacity(); got != payloadPoolMinCapacity {
		t.Fatalf("resized before enough samples: %d", got)
	}
	recordReplicationPayloadSize(64 * 1024)
	if got := payloadPoolTargetCapacity(); got <= payloadPoolMinCapacity {
		t.Fatalf("did not resize at the threshold: %d", got)
	}
}

func TestRecordPayloadSizeIgnoresNonPositive(t *testing.T) {
	resetPoolSizing(t)
	recordReplicationPayloadSize(0)
	recordReplicationPayloadSize(-5)
	if mean, samples := poolSizing.meanAndSamples(); samples != 0 || mean != 0 {
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
