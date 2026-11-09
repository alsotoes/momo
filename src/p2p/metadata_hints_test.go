package p2p

import (
	"testing"
	"time"

	"github.com/alsotoes/momo/src/storage"
	"go.uber.org/goleak"
)

// hintTransport acknowledges replicate/delete RPCs for the listed peers and
// fails sends to the rest, so a quorum can succeed while one replica is down.
type hintTransport struct {
	ackPeer   map[int32]bool
	provider  *MetadataRPCProvider
	replicate int
	deletes   int
}

func (t *hintTransport) Listen(string) error               { return nil }
func (t *hintTransport) Dial(int32, string) (*Peer, error) { return nil, nil }
func (t *hintTransport) Connect(*Peer) error               { return nil }
func (t *hintTransport) Consume() <-chan RPC               { return nil }
func (t *hintTransport) Broadcast(*RPC) int                { return 0 }
func (t *hintTransport) Peers() *PeerMap                   { return NewPeerMap() }
func (t *hintTransport) Close() error                      { return nil }
func (t *hintTransport) Addr() string                      { return "" }

func (t *hintTransport) Send(peer int32, rpc *RPC) error {
	if !t.ackPeer[peer] {
		return errPeerDown
	}
	switch rpc.Type {
	case MsgReplicateMetadata:
		args, err := DecodeReplicateMetadataArgs(rpc.Payload)
		if err != nil {
			return err
		}
		t.replicate++
		payload, _ := EncodeReplicateMetadataReply(&ReplicateMetadataReply{RequestID: args.RequestID, Success: true})
		t.provider.HandleRPC(&RPC{From: peer, Type: MsgReplicateMetadataResponse, Payload: payload})
	case MsgDeleteMetadata:
		args, err := DecodeDeleteMetadataArgs(rpc.Payload)
		if err != nil {
			return err
		}
		t.deletes++
		payload, _ := EncodeDeleteMetadataReply(&DeleteMetadataReply{RequestID: args.RequestID, Success: true})
		t.provider.HandleRPC(&RPC{From: peer, Type: MsgDeleteMetadataResponse, Payload: payload})
	}
	return nil
}

var errPeerDown = errTestPeerDown{}

type errTestPeerDown struct{}

func (errTestPeerDown) Error() string { return "peer down" }

func newHintProvider(t *testing.T, ack map[int32]bool) (*MetadataRPCProvider, *hintTransport, *storage.CASStore) {
	t.Helper()
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tr := &hintTransport{ackPeer: ack}
	p := NewMetadataRPCProvider(0, tr, NewRing([]int32{0, 1, 2}, 3), store)
	tr.provider = p
	p.RPCTimeout = 150 * time.Millisecond
	return p, tr, store
}

func TestPutMetadataStoresHintForDownedReplica(t *testing.T) {
	defer goleak.VerifyNone(t)
	p, tr, store := newHintProvider(t, map[int32]bool{1: true}) // peer 2 down, quorum 2/3 met

	reply, err := p.PutMetadata(&PutMetadataArgs{Name: "a.txt", Hash: "h", Size: 1})
	if err != nil || !reply.Success {
		t.Fatalf("quorum write should succeed: reply=%+v err=%v", reply, err)
	}
	hints, err := store.ListMetadataHints(2)
	if err != nil {
		t.Fatalf("ListMetadataHints: %v", err)
	}
	if len(hints) != 1 || hints[0].Name != "a.txt" {
		t.Fatalf("hints = %+v, want one for a.txt", hints)
	}
	if hints[0].Payload[0] != metadataHintKindReplicate {
		t.Fatalf("hint kind = %d, want replicate", hints[0].Payload[0])
	}
	// No hint for the healthy peer.
	if healthy, _ := store.ListMetadataHints(1); len(healthy) != 0 {
		t.Fatalf("healthy peer must have no hints: %+v", healthy)
	}

	// Peer recovers: replay sends the record and clears the hint.
	tr.ackPeer[2] = true
	p.ReplayMetadataHints(2)
	if remaining, _ := store.ListMetadataHints(2); len(remaining) != 0 {
		t.Fatalf("hints left after replay: %+v", remaining)
	}
	if tr.replicate == 0 {
		t.Fatal("replay did not send the record")
	}
}

func TestDeleteMetadataStoresTombstoneHint(t *testing.T) {
	defer goleak.VerifyNone(t)
	p, tr, store := newHintProvider(t, map[int32]bool{1: true})

	if err := store.PutWithMetadata("gone", "h", 0, "", nil, nil, "", nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := p.DeleteMetadata("gone"); err != nil {
		t.Fatalf("quorum delete should succeed: %v", err)
	}
	hints, _ := store.ListMetadataHints(2)
	if len(hints) != 1 || hints[0].Name != "gone" {
		t.Fatalf("hints = %+v, want one tombstone hint", hints)
	}
	if hints[0].Payload[0] != metadataHintKindDelete {
		t.Fatalf("hint kind = %d, want delete", hints[0].Payload[0])
	}

	tr.ackPeer[2] = true
	p.ReplayMetadataHints(2)
	if tr.deletes == 0 {
		t.Fatal("replay did not send the tombstone")
	}
	if remaining, _ := store.ListMetadataHints(2); len(remaining) != 0 {
		t.Fatalf("hints left after replay: %+v", remaining)
	}
}

func TestReplayKeepsHintWhenPeerStillDown(t *testing.T) {
	defer goleak.VerifyNone(t)
	p, _, store := newHintProvider(t, map[int32]bool{1: true})
	if _, err := p.PutMetadata(&PutMetadataArgs{Name: "b.txt", Hash: "h", Size: 1}); err != nil {
		t.Fatalf("PutMetadata: %v", err)
	}
	p.ReplayMetadataHints(2) // peer 2 still failing
	if remaining, _ := store.ListMetadataHints(2); len(remaining) != 1 {
		t.Fatalf("failed replay must keep the hint, got %+v", remaining)
	}
}

func TestReplayMetadataHintsNoStoreIsSafe(t *testing.T) {
	p := NewMetadataRPCProvider(0, nil, NewRing([]int32{0}, 1), nil)
	p.ReplayMetadataHints(1) // must not panic
}
