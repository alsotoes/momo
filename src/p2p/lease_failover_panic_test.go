package p2p

import "testing"

func TestSetPeerStateSurvivesPanickingObserver(t *testing.T) {
	tr := newFailoverTransport(1, 2)
	g := NewGossiper(GossipConfig{LocalID: 1}, tr)
	calls := 0
	g.OnPeerStateChange(func(int32, PeerState) { panic("observer boom") })
	g.OnPeerStateChange(func(int32, PeerState) { calls++ })

	peer := tr.pm.Get(2)
	if !g.setPeerState(peer, PeerStateSuspect) {
		t.Fatal("state must still transition")
	}
	if calls != 1 {
		t.Fatalf("later observers must still run, got %d", calls)
	}
}
