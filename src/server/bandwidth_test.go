package server

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestWriteThrottledNilLimiter(t *testing.T) {
	var buf bytes.Buffer
	data := bytes.Repeat([]byte("a"), 3*chunkSize)
	if err := writeThrottled(context.Background(), &buf, data, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), data) {
		t.Fatal("payload mismatch")
	}
}

func TestWriteThrottledChunksPreserveData(t *testing.T) {
	var buf bytes.Buffer
	data := bytes.Repeat([]byte("b"), 2*chunkSize+17)
	limiter := newBandwidthLimiter(1000) // 1 GB/s: effectively no wait
	if err := writeThrottled(context.Background(), &buf, data, limiter); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), data) {
		t.Fatal("payload mismatch")
	}
}

func TestWriteThrottledHonoursContext(t *testing.T) {
	// 1 byte/s so the second chunk can never be funded in time.
	limiter := newBandwidthLimiter(0.000001)
	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := writeThrottled(ctx, &buf, bytes.Repeat([]byte("c"), 2*chunkSize), limiter)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("no bytes should be written without budget, got %d", buf.Len())
	}
}

func TestWaitNConsumesAndRefills(t *testing.T) {
	limiter := newBandwidthLimiter(1) // 125000 B/s, burst 125000
	if err := limiter.waitN(context.Background(), int(limiter.burst)); err != nil {
		t.Fatalf("wait: %v", err)
	}
	// Budget exhausted; a small wait must refill it.
	start := time.Now()
	if err := limiter.waitN(context.Background(), 1000); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed <= 0 {
		t.Fatalf("expected to wait for refill, elapsed=%v", elapsed)
	}
}

func TestNewBandwidthLimiterRate(t *testing.T) {
	l := newBandwidthLimiter(8) // 8 Mbps = 1 MB/s
	if l.rate != 1_000_000 {
		t.Fatalf("rate = %v, want 1000000", l.rate)
	}
	if l.burst != l.rate || l.tokens != l.rate {
		t.Fatalf("burst/tokens must start full: %+v", l)
	}
}

func TestSetBandwidthLimit(t *testing.T) {
	src := &DaemonRebuildSource{}
	src.SetBandwidthLimit(10)
	if src.limiter == nil {
		t.Fatal("expected limiter for a positive cap")
	}
	src.SetBandwidthLimit(0)
	if src.limiter != nil {
		t.Fatal("zero cap must mean unlimited")
	}
	src.SetBandwidthLimit(-1)
	if src.limiter != nil {
		t.Fatal("negative cap must mean unlimited")
	}
}
