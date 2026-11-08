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

func TestDeleteMetadataEncodingRoundTrip(t *testing.T) {
	args := &DeleteMetadataArgs{RequestID: 42, ShardKey: "s", Name: "obj"}
	data, err := EncodeDeleteMetadataArgs(args)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeDeleteMetadataArgs(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.RequestID != 42 || out.ShardKey != "s" || out.Name != "obj" {
		t.Fatalf("round trip mismatch: %+v", out)
	}
	replyData, err := EncodeDeleteMetadataReply(&DeleteMetadataReply{RequestID: 42, Success: true})
	if err != nil {
		t.Fatalf("encode reply: %v", err)
	}
	reply, err := DecodeDeleteMetadataReply(replyData)
	if err != nil || !reply.Success || reply.RequestID != 42 {
		t.Fatalf("reply round trip: %+v err=%v", reply, err)
	}
	if _, err := DecodeDeleteMetadataArgs([]byte("not json")); err == nil {
		t.Fatal("malformed args must fail")
	}
	if _, err := DecodeDeleteMetadataReply([]byte("not json")); err == nil {
		t.Fatal("malformed reply must fail")
	}
}

func TestHandleDeleteMetadataRejectsMissingName(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tr := &deleteAckTransport{ackPeer: map[int32]bool{}}
	p := NewMetadataRPCProvider(0, tr, NewRing([]int32{0}, 1), store)
	tr.provider = p
	payload, _ := EncodeDeleteMetadataArgs(&DeleteMetadataArgs{RequestID: 1, Name: ""})
	p.HandleRPC(&RPC{From: 1, Type: MsgDeleteMetadata, Payload: payload})
	// Malformed payload must be ignored, not panic.
	p.HandleRPC(&RPC{From: 1, Type: MsgDeleteMetadata, Payload: []byte("bad")})
	p.HandleRPC(&RPC{From: 1, Type: MsgDeleteMetadataResponse, Payload: []byte("bad")})
}

func TestDeleteMetadataFailsWhenLocalIsNotAReplica(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	// Ring with a single replica (node 0) while this provider is node 5: the
	// only replica is remote, and it never acknowledges.
	p := NewMetadataRPCProvider(5, &deleteAckTransport{}, NewRing([]int32{0}, 1), store)
	p.RPCTimeout = 100 * time.Millisecond
	if err := p.DeleteMetadata("obj"); err == nil {
		t.Fatal("an unacknowledged remote-only delete must fail")
	}
}

func TestHandleReplicateMetadataStoresValidRecord(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tr := &deleteAckTransport{ackPeer: map[int32]bool{}}
	p := NewMetadataRPCProvider(0, tr, NewRing([]int32{0}, 1), store)
	tr.provider = p

	args := &ReplicateMetadataArgs{
		RequestID: 3,
		ShardKey:  "shard",
		Meta: map[string]interface{}{
			"Name":             "replicated",
			"Hash":             "hash-x",
			"Size":             float64(9),
			"RemotePath":       "remote/path",
			"ShardKey":         "shard",
			"VectorClock":      []interface{}{float64(1), float64(2)},
			"MetadataReplicas": []interface{}{float64(0)},
		},
	}
	payload, _ := EncodeReplicateMetadataArgs(args)
	p.HandleRPC(&RPC{From: 0, Type: MsgReplicateMetadata, Payload: payload})

	meta, err := store.GetMeta("replicated")
	if err != nil {
		t.Fatalf("replica record must be stored: %v", err)
	}
	if meta.Hash != "hash-x" || meta.Size != 9 || meta.RemotePath != "remote/path" {
		t.Fatalf("stored record mismatch: %+v", meta)
	}
	// Malformed payload must be ignored, not panic.
	p.HandleRPC(&RPC{From: 0, Type: MsgReplicateMetadata, Payload: []byte("bad")})
}
