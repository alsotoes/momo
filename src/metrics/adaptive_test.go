package metrics

import (
	"math"
	"testing"
	"time"
)

func TestEWMALearnerTracksMean(t *testing.T) {
	l := NewEWMALearner(0.5)
	for i := 0; i < 20; i++ {
		l.Add(80)
	}
	mean, samples := l.Mean()
	if samples != 20 {
		t.Fatalf("samples = %d", samples)
	}
	if math.Abs(mean-80) > 0.01 {
		t.Fatalf("mean = %v, want 80", mean)
	}
}

func TestEWMALearnerIgnoresNaN(t *testing.T) {
	l := NewEWMALearner(0.5)
	l.Add(math.NaN())
	l.Add(math.Inf(1))
	if _, samples := l.Mean(); samples != 0 {
		t.Fatalf("invalid samples must be ignored, got %d", samples)
	}
}

func TestComputeThresholdsRaisesMaxForBusyBaseline(t *testing.T) {
	got := ComputeThresholds(70, 40, 80, 5) // busy cluster: mean 80, sd 5
	if got.Max <= 70 {
		t.Fatalf("a busy baseline must raise the max above the configured value, got %v", got.Max)
	}
	if got.Max > adaptiveMaxCeiling {
		t.Fatalf("max %v exceeds the ceiling", got.Max)
	}
	if got.Min >= got.Max {
		t.Fatalf("min %v must stay below max %v", got.Min, got.Max)
	}
}

func TestComputeThresholdsKeepsQuietClusterOnConfig(t *testing.T) {
	got := ComputeThresholds(70, 40, 10, 1) // quiet cluster
	if got.Max != 70 {
		t.Fatalf("quiet cluster must keep the configured max, got %v", got.Max)
	}
	if got.Min > 40 {
		t.Fatalf("min must not exceed the configured value, got %v", got.Min)
	}
}

func TestComputeThresholdsEnforcesGapAndClamps(t *testing.T) {
	got := ComputeThresholds(60, 59, 90, 1)
	if got.Max-got.Min < 10 {
		t.Fatalf("gap %v must be at least 10", got.Max-got.Min)
	}
	if got.Min < adaptiveMinFloor || got.Max > adaptiveMaxCeiling {
		t.Fatalf("clamps violated: %+v", got)
	}
}

func TestAdaptiveThresholdsHysteresisAndMinSamples(t *testing.T) {
	a, err := NewAdaptiveThresholds(0.5, 5, 70, 40, 0, "")
	if err != nil {
		t.Fatalf("NewAdaptiveThresholds: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	// Before min_samples the configured pair is in force.
	for i := 0; i < 4; i++ {
		got := a.Observe(90)
		if got.Max != 70 || got.Min != 40 {
			t.Fatalf("sample %d used learned thresholds too early: %+v", i, got)
		}
	}
	// Steady high pressure moves the max once the sample floor is met.
	got := a.Observe(90)
	if got.Max < 70 {
		t.Fatalf("max must never drop below the configured value, got %v", got.Max)
	}
	if got.Max > adaptiveMaxCeiling || got.Min < adaptiveMinFloor {
		t.Fatalf("clamps violated: %+v", got)
	}
}

func TestAdaptiveThresholdsPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	a, err := NewAdaptiveThresholds(0.5, 2, 70, 40, time.Millisecond, dir)
	if err != nil {
		t.Fatalf("NewAdaptiveThresholds: %v", err)
	}
	for i := 0; i < 10; i++ {
		a.Observe(92)
	}
	a.MaybePersist(time.Now().Add(time.Hour))
	learned := a.Thresholds()
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	b, err := NewAdaptiveThresholds(0.5, 2, 70, 40, time.Millisecond, dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	restored := b.Thresholds()
	if restored.Max < learned.Max {
		t.Fatalf("restored max %v is below the learned %v", restored.Max, learned.Max)
	}
	if restored.Max > adaptiveMaxCeiling || restored.Min < adaptiveMinFloor {
		t.Fatalf("restored thresholds violate clamps: %+v", restored)
	}
}

func TestAdaptiveGaugesReflectActiveController(t *testing.T) {
	if _, _, _, ok := AdaptiveGauges(); ok {
		t.Fatal("no controller is active in this test binary")
	}
	a, err := NewAdaptiveThresholds(0.5, 1, 70, 40, 0, "")
	if err != nil {
		t.Fatalf("NewAdaptiveThresholds: %v", err)
	}
	setAdaptiveSnapshot(a)
	defer setAdaptiveSnapshot(nil)
	a.Observe(95)
	max, min, samples, ok := AdaptiveGauges()
	if !ok || samples == 0 {
		t.Fatalf("gauges not exported: ok=%v samples=%d", ok, samples)
	}
	if max < 70 || min < adaptiveMinFloor {
		t.Fatalf("gauge values out of range: max=%v min=%v", max, min)
	}
}

func TestDataDirFor(t *testing.T) {
	if dataDirFor(nil, 0) != "" {
		t.Fatal("nil daemons must yield no dir")
	}
}
