package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"syscall"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

// failingPusher models an unreachable peer: the pass still walks, sorts and
// evaluates every blob, so the benchmark measures the evaluation cost rather
// than the transfer.
type failingPusher struct{}

func (failingPusher) Push(context.Context, string, int) error {
	return fmt.Errorf("unreachable: %w", syscall.EHOSTUNREACH)
}

// benchRebalanceStore builds a store with n blobs, all on one node.
func benchRebalanceStore(b *testing.B, n int) (*storage.CASStore, *common.ClusterMap) {
	b.Helper()
	store, err := storage.NewCASStore(b.TempDir())
	if err != nil {
		b.Fatalf("store: %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })
	for i := 0; i < n; i++ {
		payload := []byte(fmt.Sprintf("bench-blob-%d", i))
		hash := common.HashBytes(payload)
		if err := store.Put(fmt.Sprintf("obj-%d", i), hash, int64(len(payload)), "", bytes.NewReader(payload)); err != nil {
			b.Fatalf("put: %v", err)
		}
	}
	nodes := []*common.Node{{ID: 0, Weight: 1}, {ID: 1, Weight: 1}, {ID: 2, Weight: 1}}
	return store, &common.ClusterMap{Nodes: nodes}
}

// BenchmarkRebalanceCycle measures one evaluation pass over a populated store:
// statfs, full blob walk, coldness sort, placement and push attempts. Expect
// the cost to be linear in blob count — this is the number that says whether a
// 300 s interval is sane for a large node.
func BenchmarkRebalanceCycle(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("blobs=%d", n), func(b *testing.B) {
			store, cmap := benchRebalanceStore(b, n)
			cfg := common.ConfigurationRebalance{
				Enabled: true, Interval: 300, DiskHighWater: 0, DiskLowWater: 0, MaxShedPerCycle: 1,
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := rebalanceCycle(context.Background(), store, failingPusher{}, cmap, 0, 2, cfg); err != nil {
					b.Fatalf("cycle: %v", err)
				}
			}
		})
	}
}

// BenchmarkPickShedTarget measures placement cost per candidate.
func BenchmarkPickShedTarget(b *testing.B) {
	_, cmap := benchRebalanceStore(b, 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := pickShedTarget(cmap, fmt.Sprintf("hash-%d", i), 0, 2); !ok {
			b.Fatal("no target")
		}
	}
}

// BenchmarkWriteThrottled compares the throttled writer against an unlimited
// one at a rate high enough that the bucket never waits: the delta is pure
// rebalance overhead on the push path.
func BenchmarkWriteThrottled(b *testing.B) {
	data := bytes.Repeat([]byte("x"), 1<<20)
	// io.Discard isolates the limiter/token-bucket cost from buffer growth.
	dst := io.Discard
	b.Run("unlimited", func(b *testing.B) {
		b.SetBytes(int64(len(data)))
		for i := 0; i < b.N; i++ {
			if err := writeThrottled(context.Background(), dst, data, nil); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("throttled-10Gbps", func(b *testing.B) {
		limiter := newBandwidthLimiter(10_000)
		b.SetBytes(int64(len(data)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := writeThrottled(context.Background(), dst, data, limiter); err != nil {
				b.Fatal(err)
			}
		}
	})
}
