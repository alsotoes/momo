package p2p

import (
	"sync"
	"testing"
	"time"
)

// renewTransport grants every lease request back to the caller, echoing the
// request's lease ID. It lets a single-node test exercise acquire and renew.
type renewTransport struct {
	mu       sync.Mutex
	pm       *PeerMap
	provider *LeaseManager
	ids      []uint64
}

func newRenewTransport(peerIDs ...int32) *renewTransport {
	pm := NewPeerMap()
	for _, id := range peerIDs {
		pm.Add(NewPeer(id, "127.0.0.1:0"))
	}
	return &renewTransport{pm: pm}
}

func (t *renewTransport) Listen(string) error               { return nil }
func (t *renewTransport) Dial(int32, string) (*Peer, error) { return nil, nil }
func (t *renewTransport) Connect(*Peer) error               { return nil }
func (t *renewTransport) Consume() <-chan RPC               { return nil }
func (t *renewTransport) Broadcast(rpc *RPC) int {
	for _, p := range t.pm.All() {
		_ = t.Send(p.ID, rpc)
	}
	return t.pm.Count()
}
func (t *renewTransport) Peers() *PeerMap { return t.pm }
func (t *renewTransport) Close() error    { return nil }
func (t *renewTransport) Addr() string    { return "" }

func (t *renewTransport) Send(peer int32, rpc *RPC) error {
	if rpc.Type != MsgLeaseRequest {
		return nil
	}
	payload, err := DecodeLeasePayload(rpc.Payload)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.ids = append(t.ids, payload.LeaseID)
	t.mu.Unlock()
	grant := &LeasePayload{LeaseID: payload.LeaseID, Key: payload.Key, Expiry: payload.Expiry}
	t.provider.handleLeaseGrant(&RPC{From: peer, Type: MsgLeaseGrant, Payload: grant.Encode()})
	return nil
}

func (t *renewTransport) lastID() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.ids) == 0 {
		return 0
	}
	return t.ids[len(t.ids)-1]
}

func TestLeaseRenewKeepsSameID(t *testing.T) {
	tr := newRenewTransport(1, 2)
	lm := NewLeaseManager(0, tr)
	tr.provider = lm

	lease, err := lm.Acquire("metrics-controller", time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	firstExpiry := lease.Expiry

	time.Sleep(2 * time.Millisecond)
	renewed, err := lm.Renew(lease, time.Second)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if renewed.LeaseID != lease.LeaseID {
		t.Fatalf("renew changed the lease ID: %d -> %d", lease.LeaseID, renewed.LeaseID)
	}
	if !renewed.Expiry.After(firstExpiry) {
		t.Fatalf("renew did not extend the expiry: %v -> %v", firstExpiry, renewed.Expiry)
	}
	if tr.lastID() != lease.LeaseID {
		t.Fatalf("peer saw lease ID %d, want %d", tr.lastID(), lease.LeaseID)
	}
	// The manager tracks exactly one lease for this key.
	lm.heldMu.Lock()
	held := len(lm.held)
	lm.heldMu.Unlock()
	if held != 1 {
		t.Fatalf("held leases = %d, want 1", held)
	}
}

func TestLeaseRenewRejectsInvalidLease(t *testing.T) {
	lm := NewLeaseManager(0, newRenewTransport(1))
	if _, err := lm.Renew(nil, time.Second); err == nil {
		t.Fatal("expected an error for a nil lease")
	}
	if _, err := lm.Renew(&Lease{Key: "k"}, time.Second); err == nil {
		t.Fatal("expected an error for a zero lease ID")
	}
}

// TestLeaseGrantRejectsDifferentHolderAndAcceptsRenewal pins the peer-side rule
// that makes renewal safe: same ID extends, a different holder is denied.
func TestLeaseGrantRejectsDifferentHolderAndAcceptsRenewal(t *testing.T) {
	tr := newRenewTransport()
	lm := NewLeaseManager(1, tr)
	future := time.Now().Add(time.Minute).UnixNano()

	lm.handleLeaseRequest(&RPC{From: 2, Type: MsgLeaseRequest, Payload: (&LeasePayload{LeaseID: 7, Key: "k", Expiry: future}).Encode()})
	lm.grantedMu.Lock()
	if lm.granted["k"] != future {
		lm.grantedMu.Unlock()
		t.Fatal("first request must be granted")
	}
	lm.grantedMu.Unlock()

	// Another holder is denied while the lease is active.
	lm.handleLeaseRequest(&RPC{From: 3, Type: MsgLeaseRequest, Payload: (&LeasePayload{LeaseID: 8, Key: "k", Expiry: future + 1}).Encode()})
	lm.grantedMu.Lock()
	if lm.granted["k"] != future || lm.grantedID["k"] != 7 {
		lm.grantedMu.Unlock()
		t.Fatalf("a different holder must not take the key: expiry=%d id=%d", lm.granted["k"], lm.grantedID["k"])
	}
	lm.grantedMu.Unlock()

	// The same holder renews with a later expiry.
	later := future + int64(time.Minute)
	lm.handleLeaseRequest(&RPC{From: 2, Type: MsgLeaseRequest, Payload: (&LeasePayload{LeaseID: 7, Key: "k", Expiry: later}).Encode()})
	lm.grantedMu.Lock()
	defer lm.grantedMu.Unlock()
	if lm.granted["k"] != later || lm.grantedID["k"] != 7 {
		t.Fatalf("renewal not applied: expiry=%d id=%d", lm.granted["k"], lm.grantedID["k"])
	}
}
