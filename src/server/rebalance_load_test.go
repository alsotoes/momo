package server

import (
	"testing"

	"github.com/alsotoes/momo/src/p2p"
)

func withPeerLoad(t *testing.T, fn func(int32) (p2p.PeerLoad, bool)) {
	t.Helper()
	setPeerLoadSource(fn)
	t.Cleanup(func() { setPeerLoadSource(nil) })
}

// TestPickShedTargetPrefersLeastLoadedPeer proves shedding does not push bytes
// onto a saturated replica when a healthy alternative exists.
func TestPickShedTargetPrefersLeastLoadedPeer(t *testing.T) {
	cmap := testCmap3(t)
	withPeerLoad(t, func(id int32) (p2p.PeerLoad, bool) {
		switch id {
		case 1:
			return p2p.PeerLoad{DiskUsedPercent: 96, InflightRequests: 10}, true // saturated disk
		case 2:
			return p2p.PeerLoad{DiskUsedPercent: 20, InflightRequests: 3}, true
		}
		return p2p.PeerLoad{}, false
	})
	target, ok := pickShedTarget(cmap, "hash-load", 0, 3)
	if !ok {
		t.Fatal("expected a target")
	}
	if target == 1 {
		t.Fatal("a disk-saturated peer must not be chosen while a healthy peer exists")
	}
}

func TestPickShedTargetUnknownLoadFallsBackToFirstEligible(t *testing.T) {
	cmap := testCmap(t)
	withPeerLoad(t, func(int32) (p2p.PeerLoad, bool) { return p2p.PeerLoad{}, false })
	if _, ok := pickShedTarget(cmap, "hash-unknown", 0, 2); !ok {
		t.Fatal("with no load reports the first eligible peer must be used")
	}
}

func TestPickShedTargetUsesSaturatedPeerAsLastResort(t *testing.T) {
	cmap := testCmap3(t)
	withPeerLoad(t, func(int32) (p2p.PeerLoad, bool) {
		return p2p.PeerLoad{DiskUsedPercent: 99, InflightRequests: 5000}, true
	})
	if _, ok := pickShedTarget(cmap, "hash-saturated", 0, 3); !ok {
		t.Fatal("shedding must still make progress when every peer is saturated")
	}
}

func TestPickShedTargetBreaksTiesByInflight(t *testing.T) {
	cmap := testCmap3(t)
	withPeerLoad(t, func(id int32) (p2p.PeerLoad, bool) {
		if id == 1 {
			return p2p.PeerLoad{DiskUsedPercent: 30, InflightRequests: 50}, true
		}
		return p2p.PeerLoad{DiskUsedPercent: 30, InflightRequests: 1}, true
	})
	target, ok := pickShedTarget(cmap, "hash-tie", 0, 3)
	if !ok {
		t.Fatal("expected a target")
	}
	// Node 2 has the lower in-flight count at equal disk usage.
	if target != 2 {
		t.Fatalf("expected the less busy peer, got %d", target)
	}
}
