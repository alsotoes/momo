package p2p

import (
	"testing"
	"time"

	"github.com/alsotoes/momo/src/storage"
)

func TestClampRPCTimeout(t *testing.T) {
	cases := []struct {
		in, want time.Duration
	}{
		{time.Millisecond, MetadataRPCTimeoutFloor},
		{time.Hour, MetadataRPCTimeoutCeiling},
		{2 * time.Second, 2 * time.Second},
	}
	for _, tc := range cases {
		if got := clampRPCTimeout(tc.in); got != tc.want {
			t.Fatalf("clampRPCTimeout(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestAdaptiveRPCTimeoutFollowsObservedRTT(t *testing.T) {
	peers := NewPeerMap()
	p := &Peer{ID: 1}
	p.SetRTT(20 * time.Millisecond)
	peers.Add(p)

	// 10 × 20ms = 200ms, raised to the 500ms floor.
	if got := adaptiveRPCTimeout(peers); got != MetadataRPCTimeoutFloor {
		t.Fatalf("timeout = %v, want the floor %v", got, MetadataRPCTimeoutFloor)
	}
}

func TestAdaptiveRPCTimeoutSlowClusterBounded(t *testing.T) {
	peers := NewPeerMap()
	p := &Peer{ID: 1}
	p.SetRTT(3 * time.Second) // pathological link
	peers.Add(p)
	if got := adaptiveRPCTimeout(peers); got != MetadataRPCTimeoutCeiling {
		t.Fatalf("timeout = %v, want ceiling %v", got, MetadataRPCTimeoutCeiling)
	}
}

func TestAdaptiveRPCTimeoutUnknownRTT(t *testing.T) {
	if got := adaptiveRPCTimeout(NewPeerMap()); got != 0 {
		t.Fatalf("unknown RTT must yield 0 (use default), got %v", got)
	}
	if got := adaptiveRPCTimeout(nil); got != 0 {
		t.Fatalf("nil peer map must yield 0, got %v", got)
	}
}

func TestRPCTimeoutPrecedence(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	tr := &deleteAckTransport{}
	provider := NewMetadataRPCProvider(0, tr, NewRing([]int32{0}, 1), store)

	// No transport peers, no override: published default.
	if got := provider.rpcTimeout(); got != MetadataRPCTimeout {
		t.Fatalf("default timeout = %v", got)
	}
	// Explicit override wins over everything.
	provider.RPCTimeout = 42 * time.Second
	if got := provider.rpcTimeout(); got != 42*time.Second {
		t.Fatalf("override ignored: %v", got)
	}
}
