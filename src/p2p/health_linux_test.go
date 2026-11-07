//go:build linux

package p2p

import "testing"

func TestReadCPUStatFromProc(t *testing.T) {
	busy, total, err := readCPUStat()
	if err != nil {
		t.Fatalf("readCPUStat: %v", err)
	}
	if total == 0 {
		t.Fatal("total jiffies must be non-zero")
	}
	if busy > total {
		t.Fatalf("busy %d exceeds total %d", busy, total)
	}
}

func TestReadMemPercentFromProc(t *testing.T) {
	pct, err := readMemPercent()
	if err != nil {
		t.Fatalf("readMemPercent: %v", err)
	}
	if pct < 0 || pct > 100 {
		t.Fatalf("memory percent out of range: %f", pct)
	}
}

func TestDefaultHealthSamplerDelta(t *testing.T) {
	sampler := DefaultHealthSampler()
	first := sampler()
	if first.Goroutines <= 0 {
		t.Fatalf("goroutines must be reported, got %d", first.Goroutines)
	}
	if first.CPUPercent != 0 {
		t.Fatalf("the first sample has no delta and must report 0, got %f", first.CPUPercent)
	}
	second := sampler()
	if second.CPUPercent < 0 || second.CPUPercent > 100 {
		t.Fatalf("cpu percent out of range: %f", second.CPUPercent)
	}
	if second.MemPercent < 0 || second.MemPercent > 100 {
		t.Fatalf("memory percent out of range: %f", second.MemPercent)
	}
}
