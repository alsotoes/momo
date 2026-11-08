package p2p

import (
	"testing"
	"time"

	"github.com/alsotoes/momo/src/storage"
	"go.uber.org/goleak"
)

// deleteAckTransport acks DeleteMetadata from the listed peers only.
type deleteAckTransport struct {
	ackPeer  map[int32]bool
	provider *MetadataRPCProvider
}

func (t *deleteAckTransport) Listen(string) error               { return nil }
func (t *deleteAckTransport) Dial(int32, string) (*Peer, error) { return nil, nil }
func (t *deleteAckTransport) Connect(*Peer) error               { return nil }
func (t *deleteAckTransport) Consume() <-chan RPC               { return nil }
func (t *deleteAckTransport) Broadcast(*RPC) int                { return 0 }
func (t *deleteAckTransport) Peers() *PeerMap                   { return NewPeerMap() }
func (t *deleteAckTransport) Close() error                      { return nil }
func (t *deleteAckTransport) Addr() string                      { return "" }

func (t *deleteAckTransport) Send(peer int32, rpc *RPC) error {
	if rpc.Type != MsgDeleteMetadata || !t.ackPeer[peer] {
		return nil
	}
	args, err := DecodeDeleteMetadataArgs(rpc.Payload)
	if err != nil {
		return err
	}
	reply := &DeleteMetadataReply{RequestID: args.RequestID, Success: true}
	payload, _ := EncodeDeleteMetadataReply(reply)
	t.provider.HandleRPC(&RPC{From: peer, Type: MsgDeleteMetadataResponse, Payload: payload})
	return nil
}

func TestDeleteMetadataQuorum(t *testing.T) {
	defer goleak.VerifyNone(t)
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tr := &deleteAckTransport{ackPeer: map[int32]bool{1: true}}
	p := NewMetadataRPCProvider(0, tr, NewRing([]int32{0, 1, 2}, 3), store)
	tr.provider = p
	p.RPCTimeout = 200 * time.Millisecond

	if err := store.PutWithMetadata("gone", "h", 0, "", nil, nil, "", nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := p.DeleteMetadata("gone"); err != nil {
		t.Fatalf("DeleteMetadata: %v", err)
	}
	if _, err := store.GetMeta("gone"); err == nil {
		t.Fatal("the local replica must hold the tombstone")
	}
}

func TestDeleteMetadataFailsBelowQuorum(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tr := &deleteAckTransport{ackPeer: map[int32]bool{}}
	p := NewMetadataRPCProvider(0, tr, NewRing([]int32{0, 1, 2}, 3), store)
	tr.provider = p
	p.RPCTimeout = 150 * time.Millisecond
	if err := p.DeleteMetadata("gone"); err == nil {
		t.Fatal("expected a quorum error")
	}
}

func TestDeleteMetadataRejectsEmptyName(t *testing.T) {
	store, _ := storage.NewCASStore(t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	p := NewMetadataRPCProvider(0, &deleteAckTransport{}, NewRing([]int32{0}, 1), store)
	if err := p.DeleteMetadata(""); err == nil {
		t.Fatal("empty name must be rejected")
	}
}

func TestTombstoneReplicaDoesNotRepropagate(t *testing.T) {
	defer goleak.VerifyNone(t)
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fired := make(chan string, 1)
	store.SetMetadataDeleteHook(func(name string) { fired <- name })

	if err := store.PutWithMetadata("obj", "h2", 0, "", nil, nil, "", nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A replica-applied tombstone must not fire the propagation hook.
	if err := store.DeleteReplica("obj"); err != nil {
		t.Fatalf("DeleteReplica: %v", err)
	}
	select {
	case name := <-fired:
		t.Fatalf("replica delete re-propagated %q", name)
	case <-time.After(100 * time.Millisecond):
	}
	// A normal delete still propagates.
	if err := store.PutWithMetadata("obj2", "h3", 0, "", nil, nil, "", nil); err != nil {
		t.Fatalf("seed2: %v", err)
	}
	if err := store.Delete("obj2"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	select {
	case name := <-fired:
		if name != "obj2" {
			t.Fatalf("propagated %q, want obj2", name)
		}
	case <-time.After(time.Second):
		t.Fatal("local delete must propagate")
	}
}
