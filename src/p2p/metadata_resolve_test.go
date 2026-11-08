package p2p

import (
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/storage"
	"go.uber.org/goleak"
)

// resolveTransport answers ResolveMetadata from a per-peer map and records
// which peers received a read-repair replication.
type resolveTransport struct {
	replies  map[int32]*ResolveMetadataReply
	repaired map[int32]bool
	provider *MetadataRPCProvider
}

func (t *resolveTransport) Listen(string) error               { return nil }
func (t *resolveTransport) Dial(int32, string) (*Peer, error) { return nil, nil }
func (t *resolveTransport) Connect(*Peer) error               { return nil }
func (t *resolveTransport) Consume() <-chan RPC               { return nil }
func (t *resolveTransport) Broadcast(*RPC) int                { return 0 }
func (t *resolveTransport) Peers() *PeerMap                   { return NewPeerMap() }
func (t *resolveTransport) Close() error                      { return nil }
func (t *resolveTransport) Addr() string                      { return "" }

func (t *resolveTransport) Send(peer int32, rpc *RPC) error {
	switch rpc.Type {
	case MsgResolveMetadata:
		args, err := DecodeResolveMetadataArgs(rpc.Payload)
		if err != nil {
			return err
		}
		reply := t.replies[peer]
		if reply == nil {
			return nil
		}
		out := *reply
		out.RequestID = args.RequestID
		payload, _ := EncodeResolveMetadataReply(&out)
		t.provider.HandleRPC(&RPC{From: peer, Type: MsgResolveMetadataResponse, Payload: payload})
	case MsgReplicateMetadata:
		t.repaired[peer] = true
		args, err := DecodeReplicateMetadataArgs(rpc.Payload)
		if err != nil {
			return err
		}
		reply := &ReplicateMetadataReply{RequestID: args.RequestID, Success: true}
		payload, _ := EncodeReplicateMetadataReply(reply)
		t.provider.HandleRPC(&RPC{From: peer, Type: MsgReplicateMetadataResponse, Payload: payload})
	}
	return nil
}

func newResolveProvider(t *testing.T, local int32, ids []int32, m int, replies map[int32]*ResolveMetadataReply) (*MetadataRPCProvider, *resolveTransport) {
	t.Helper()
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tr := &resolveTransport{replies: replies, repaired: map[int32]bool{}}
	p := NewMetadataRPCProvider(local, tr, NewRing(ids, m), store)
	tr.provider = p
	p.RPCTimeout = 300 * time.Millisecond
	return p, tr
}

func TestResolveMetadataPrefersDominantVectorClock(t *testing.T) {
	defer goleak.VerifyNone(t)
	// Local node 0 is a replica; peer 1 is stale, peer 2 dominates.
	p, tr := newResolveProvider(t, 0, []int32{0, 1, 2}, 3, map[int32]*ResolveMetadataReply{
		1: {Hash: "old", Size: 1, ModTime: 10, VectorClock: []uint64{1, 0, 0}},
		2: {Hash: "new", Size: 2, ModTime: 20, VectorClock: []uint64{1, 1, 0}},
	})
	if err := p.store.PutWithMetadata("obj", "old", 1, "", nil, []uint64{1, 0, 0}, "s", []int32{0, 1, 2}); err != nil {
		t.Fatalf("seed local: %v", err)
	}
	reply, err := p.ResolveMetadata(&ResolveMetadataArgs{Name: "obj"})
	if err != nil {
		t.Fatalf("ResolveMetadata: %v", err)
	}
	if reply.Hash != "new" {
		t.Fatalf("expected the dominating record, got %q", reply.Hash)
	}
	if !tr.repaired[1] {
		t.Fatal("the stale replica must be repaired")
	}
	if dm, _ := p.store.GetDistributedMeta("obj"); dm.Hash != "new" {
		t.Fatalf("local replica must be updated, got %q", dm.Hash)
	}
}

func TestResolveMetadataConcurrentConflictIsDeterministic(t *testing.T) {
	defer goleak.VerifyNone(t)
	// Neither clock dominates; the newer modtime must win, and the choice must
	// not depend on arrival order.
	p, _ := newResolveProvider(t, 5, []int32{0, 1}, 2, map[int32]*ResolveMetadataReply{
		0: {Hash: "a", Size: 1, ModTime: 5, VectorClock: []uint64{1, 0}},
		1: {Hash: "b", Size: 1, ModTime: 9, VectorClock: []uint64{0, 1}},
	})
	reply, err := p.ResolveMetadata(&ResolveMetadataArgs{Name: "obj"})
	if err != nil {
		t.Fatalf("ResolveMetadata: %v", err)
	}
	if reply.Hash != "b" {
		t.Fatalf("newer record must win, got %q", reply.Hash)
	}
}

func TestResolveMetadataTieBreaksOnHash(t *testing.T) {
	defer goleak.VerifyNone(t)
	p, _ := newResolveProvider(t, 5, []int32{0, 1}, 2, map[int32]*ResolveMetadataReply{
		0: {Hash: "aaa", Size: 1, ModTime: 5, VectorClock: []uint64{1, 0}},
		1: {Hash: "zzz", Size: 1, ModTime: 5, VectorClock: []uint64{0, 1}},
	})
	reply, err := p.ResolveMetadata(&ResolveMetadataArgs{Name: "obj"})
	if err != nil {
		t.Fatalf("ResolveMetadata: %v", err)
	}
	if reply.Hash != "zzz" {
		t.Fatalf("equal modtimes must break on the greater hash, got %q", reply.Hash)
	}
}

func TestResolveMetadataNotFoundAnywhere(t *testing.T) {
	p, _ := newResolveProvider(t, 5, []int32{0, 1}, 2, map[int32]*ResolveMetadataReply{})
	_, err := p.ResolveMetadata(&ResolveMetadataArgs{Name: "missing"})
	if err == nil {
		t.Fatal("expected not-found")
	}
	if !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("want ENOENT, got %v", err)
	}
}

func TestResolveMetadataRejectsEmptyName(t *testing.T) {
	p, _ := newResolveProvider(t, 0, []int32{0}, 1, nil)
	if _, err := p.ResolveMetadata(&ResolveMetadataArgs{}); err == nil {
		t.Fatal("empty name must be rejected")
	}
}

func TestCompareVectorClocks(t *testing.T) {
	cases := []struct {
		a, b []uint64
		want int
	}{
		{[]uint64{1}, []uint64{1}, 0},
		{[]uint64{2}, []uint64{1}, 1},
		{[]uint64{1}, []uint64{2}, -1},
		{[]uint64{2, 0}, []uint64{1, 1}, 0}, // concurrent
		{[]uint64{1, 2}, []uint64{0, 2}, 1},
		{[]uint64{1}, []uint64{1, 0}, 0},
	}
	for _, tc := range cases {
		if got := compareVectorClocks(tc.a, tc.b); got != tc.want {
			t.Fatalf("%v vs %v: got %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
