package p2p

import (
	"sync"
	"testing"
)

// failoverTransport is a Transport stub with a controllable peer set.
type failoverTransport struct {
	mu      sync.Mutex
	pm      *PeerMap
	replies map[int32]bool // peers that grant lease requests
	sent    []RPC
}

func newFailoverTransport(localID int32, peerIDs ...int32) *failoverTransport {
	pm := NewPeerMap()
	for _, id := range peerIDs {
		pm.Add(NewPeer(id, "127.0.0.1:0"))
	}
	replies := map[int32]bool{}
	for _, id := range peerIDs {
		replies[id] = true
	}
	return &failoverTransport{pm: pm, replies: replies}
}

func (t *failoverTransport) Listen(string) error               { return nil }
func (t *failoverTransport) Dial(int32, string) (*Peer, error) { return nil, nil }
func (t *failoverTransport) Connect(*Peer) error               { return nil }
func (t *failoverTransport) Consume() <-chan RPC               { return nil }
func (t *failoverTransport) Broadcast(rpc *RPC) int {
	t.mu.Lock()
	t.sent = append(t.sent, *rpc)
	n := t.pm.Count()
	t.mu.Unlock()
	return n
}
func (t *failoverTransport) Peers() *PeerMap { return t.pm }
func (t *failoverTransport) Close() error    { return nil }
func (t *failoverTransport) Addr() string    { return "" }

// Send grants the lease request unless the peer is in the configured grant set.
func (t *failoverTransport) Send(peerID int32, rpc *RPC) error {
	t.mu.Lock()
	t.sent = append(t.sent, *rpc)
	grant := t.replies[peerID]
	t.mu.Unlock()
	if rpc.Type != MsgLeaseRequest {
		return nil
	}
	payload, err := DecodeLeasePayload(rpc.Payload)
	if err != nil {
		return err
	}
	lm := &LeaseManager{localID: peerID, transport: t}
	lm.held[payload.LeaseID] = &Lease{LeaseID: payload.LeaseID, HolderID: peerID}
	_ = grant
	return nil
}

func (t *failoverTransport) grantCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, rpc := range t.sent {
		if rpc.Type == MsgLeaseRelease {
			n++
		}
	}
	return n
}

func (t *failoverTransport) setAlive(id int32, alive bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if alive {
		t.pm.Add(NewPeer(id, "127.0.0.1:0"))
		return
	}
	t.pm.Remove(id)
}

func TestLeaseManagerRevokesOnPeerFailure(t *testing.T) {
	tr := newFailoverTransport(1, 2)
	lm := NewLeaseManager(1, tr)
	lm.held[42] = &Lease{LeaseID: 42, HolderID: 1, Key: "metrics-controller", QuorumSize: 1}

	// The only granter is gone: the lease must be released immediately.
	tr.setAlive(2, false)
	if err := lm.OnPeerFailure(2); err != nil {
		t.Fatalf("OnPeerFailure: %v", err)
	}

	if got := lm.ActiveLeases(); got != 0 {
		t.Fatalf("lease must be dropped on quorum loss, %d remain", got)
	}
	if got := tr.grantCount(); got != 1 {
		t.Fatalf("expected one release broadcast, got %d", got)
	}
}

func TestLeaseManagerKeepsLeaseWhenQuorumSurvives(t *testing.T) {
	tr := newFailoverTransport(1, 2, 3)
	lm := NewLeaseManager(1, tr)
	lm.held[7] = &Lease{LeaseID: 7, HolderID: 1, Key: "replication-propagator", QuorumSize: 1}

	// One of two peers failed: a single reachable peer still satisfies a quorum
	// of one, so the lease stays.
	tr.setAlive(2, false)
	if err := lm.OnPeerFailure(2); err != nil {
		t.Fatalf("OnPeerFailure: %v", err)
	}

	if got := lm.ActiveLeases(); got != 1 {
		t.Fatalf("lease must survive while quorum holds, %d remain", got)
	}
}

func TestLeaseManagerIgnoresSelfAndUnknown(t *testing.T) {
	tr := newFailoverTransport(1)
	lm := NewLeaseManager(1, tr)
	lm.held[9] = &Lease{LeaseID: 9, HolderID: 1, Key: "k", QuorumSize: 0}

	_ = lm.OnPeerFailure(1)  // self
	_ = lm.OnPeerFailure(99) // unknown
	if got := lm.ActiveLeases(); got != 1 {
		t.Fatalf("zero-quorum lease must survive, %d remain", got)
	}
}

func TestGossiperNotifiesStateObservers(t *testing.T) {
	tr := newFailoverTransport(1, 2)
	g := NewGossiper(GossipConfig{LocalID: 1}, tr)

	var mu sync.Mutex
	var seen []PeerState
	g.OnPeerStateChange(func(_ int32, state PeerState) {
		mu.Lock()
		seen = append(seen, state)
		mu.Unlock()
	})

	peer := tr.pm.Get(2)
	g.setPeerState(peer, PeerStateSuspect)
	g.setPeerState(peer, PeerStateAlive) // recovery is not reported
	g.setPeerState(peer, PeerStateOffline)

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("expected suspicious+offline notifications, got %v", seen)
	}
	if seen[0] != PeerStateSuspect || seen[1] != PeerStateOffline {
		t.Fatalf("unexpected sequence: %v", seen)
	}
}

func TestGossiperSetPeerStateIdempotent(t *testing.T) {
	tr := newFailoverTransport(1, 2)
	g := NewGossiper(GossipConfig{LocalID: 1}, tr)
	calls := 0
	g.OnPeerStateChange(func(int32, PeerState) { calls++ })

	peer := tr.pm.Get(2)
	if !g.setPeerState(peer, PeerStateSuspect) {
		t.Fatal("first transition should report a change")
	}
	if g.setPeerState(peer, PeerStateSuspect) {
		t.Fatal("repeated transition must be a no-op")
	}
	if calls != 1 {
		t.Fatalf("observer calls = %d, want 1", calls)
	}
}
