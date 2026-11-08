package p2p

import (
	"errors"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/storage"
)

// quorumTransport acks ReplicateMetadata from the peers in ackPeer and stays
// silent for the rest, simulating a partially degraded replica set.
type quorumTransport struct {
	ackPeer  map[int32]bool
	provider *MetadataRPCProvider
	acks     atomic.Int64
}

func newQuorumTransport(ack ...int32) *quorumTransport {
	t := &quorumTransport{ackPeer: map[int32]bool{}}
	for _, id := range ack {
		t.ackPeer[id] = true
	}
	return t
}

func (t *quorumTransport) Listen(string) error               { return nil }
func (t *quorumTransport) Dial(int32, string) (*Peer, error) { return nil, nil }
func (t *quorumTransport) Connect(*Peer) error               { return nil }
func (t *quorumTransport) Consume() <-chan RPC               { return nil }
func (t *quorumTransport) Broadcast(*RPC) int                { return 0 }
func (t *quorumTransport) Peers() *PeerMap                   { return NewPeerMap() }
func (t *quorumTransport) Close() error                      { return nil }
func (t *quorumTransport) Addr() string                      { return "" }

func (t *quorumTransport) Send(peer int32, rpc *RPC) error {
	if rpc.Type != MsgReplicateMetadata || !t.ackPeer[peer] {
		return nil
	}
	args, err := DecodeReplicateMetadataArgs(rpc.Payload)
	if err != nil {
		return err
	}
	t.acks.Add(1)
	reply := &ReplicateMetadataReply{RequestID: args.RequestID, Success: true}
	payload, _ := EncodeReplicateMetadataReply(reply)
	t.provider.HandleRPC(&RPC{From: peer, Type: MsgReplicateMetadataResponse, Payload: payload})
	return nil
}

func newQuorumProvider(t *testing.T, local int32, ids []int32, m int, ack ...int32) (*MetadataRPCProvider, *storage.CASStore, *quorumTransport) {
	t.Helper()
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tr := newQuorumTransport(ack...)
	tr.provider = NewMetadataRPCProvider(local, tr, NewRing(ids, m), store)
	tr.provider.RPCTimeout = 200 * time.Millisecond
	return tr.provider, store, tr
}

func TestPutMetadataQuorumReachedWithLocalAndOnePeer(t *testing.T) {
	p, store, _ := newQuorumProvider(t, 0, []int32{0, 1, 2}, 3, 1) // peer 2 silent
	args := &PutMetadataArgs{
		Name: "obj", Hash: "abc", Size: 3, MetadataReplicas: []int32{0, 1, 2},
	}
	reply, err := p.PutMetadata(args)
	if err != nil {
		t.Fatalf("PutMetadata: %v", err)
	}
	if !reply.Success {
		t.Fatalf("expected success, got %+v", reply)
	}
	if len(reply.ExistingVClock) <= 0 || reply.ExistingVClock[0] != 1 {
		t.Fatalf("local vector clock must advance, got %v", reply.ExistingVClock)
	}
	meta, err := store.GetMeta("obj")
	if err != nil {
		t.Fatalf("local replica must hold the metadata: %v", err)
	}
	if meta.Hash != "abc" {
		t.Fatalf("stored hash = %q", meta.Hash)
	}
}

func TestPutMetadataFailsBelowQuorum(t *testing.T) {
	p, _, _ := newQuorumProvider(t, 0, []int32{0, 1, 2}, 3) // no peer acks
	args := &PutMetadataArgs{Name: "obj", Hash: "abc", MetadataReplicas: []int32{0, 1, 2}}
	reply, err := p.PutMetadata(args)
	if err == nil {
		t.Fatal("expected a quorum error")
	}
	if !errors.Is(err, syscall.EIO) {
		t.Fatalf("want EIO, got %v", err)
	}
	if reply == nil || reply.Success {
		t.Fatalf("reply must report failure: %+v", reply)
	}
}

func TestPutMetadataSingleReplicaIsLocalOnly(t *testing.T) {
	p, store, _ := newQuorumProvider(t, 0, []int32{0}, 1)
	reply, err := p.PutMetadata(&PutMetadataArgs{Name: "solo", Hash: "h", MetadataReplicas: []int32{0}})
	if err != nil || !reply.Success {
		t.Fatalf("single replica write: reply=%+v err=%v", reply, err)
	}
	if _, err := store.GetMeta("solo"); err != nil {
		t.Fatalf("local store: %v", err)
	}
}

func TestPutMetadataDerivesShardKeyAndReplicas(t *testing.T) {
	p, _, _ := newQuorumProvider(t, 0, []int32{0, 1, 2}, 3, 1)
	args := &PutMetadataArgs{Name: "auto", Hash: "h"}
	if _, err := p.PutMetadata(args); err != nil {
		t.Fatalf("PutMetadata: %v", err)
	}
	if args.ShardKey == "" {
		t.Fatal("shard key must be derived from the name")
	}
	if len(args.MetadataReplicas) == 0 {
		t.Fatal("replica set must be resolved from the ring")
	}
}

func TestReplicateMetadataRejectsNamelessRecord(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tr := newQuorumTransport()
	p := NewMetadataRPCProvider(0, tr, NewRing([]int32{0, 1}, 1), store)
	args := &ReplicateMetadataArgs{RequestID: 7, ShardKey: "s", Meta: map[string]interface{}{"Name": "", "Hash": "h"}}
	payload, _ := EncodeReplicateMetadataArgs(args)
	// Fail-closed: no record is written and the reply reports the rejection.
	p.handleReplicateMetadata(&RPC{From: 1, Payload: payload})
	if _, err := store.GetMeta(""); err == nil {
		t.Fatal("a nameless record must not be stored")
	}
}

func TestReplicateMetadataReplyCorrelation(t *testing.T) {
	p, _, _ := newQuorumProvider(t, 0, []int32{0, 1}, 2)
	// A reply for an unknown request must be dropped without blocking.
	payload, _ := EncodeReplicateMetadataReply(&ReplicateMetadataReply{RequestID: 999, Success: true})
	p.HandleRPC(&RPC{From: 1, Type: MsgReplicateMetadataResponse, Payload: payload})
}
