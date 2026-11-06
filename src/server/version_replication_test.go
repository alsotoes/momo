package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/p2p"
	"github.com/alsotoes/momo/src/storage"
)

// recordingP2PTransport captures version-replication sends for assertions.
type recordingP2PTransport struct {
	mu   sync.Mutex
	sent []p2p.RPC
}

func (r *recordingP2PTransport) Listen(string) error                   { return nil }
func (r *recordingP2PTransport) Dial(int32, string) (*p2p.Peer, error) { return nil, nil }
func (r *recordingP2PTransport) Connect(*p2p.Peer) error               { return nil }
func (r *recordingP2PTransport) Consume() <-chan p2p.RPC               { return nil }
func (r *recordingP2PTransport) Broadcast(*p2p.RPC) int                { return 0 }
func (r *recordingP2PTransport) Peers() *p2p.PeerMap                   { return p2p.NewPeerMap() }
func (r *recordingP2PTransport) Close() error                          { return nil }
func (r *recordingP2PTransport) Addr() string                          { return "" }

func (r *recordingP2PTransport) Send(_ int32, rpc *p2p.RPC) error {
	r.mu.Lock()
	r.sent = append(r.sent, *rpc)
	r.mu.Unlock()
	return nil
}

func (r *recordingP2PTransport) sentCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

func TestInstallVersionReplication_SkipsSingleNode(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ft := &recordingP2PTransport{}
	provider := p2p.NewMetadataRPCProvider(0, ft, p2p.NewRing([]int32{0}, 1), store)
	cmap := &common.ClusterMap{Nodes: []*common.Node{{ID: 0, Weight: 1}}}
	installVersionReplication(context.Background(), store, cmap, provider, 0, 1)

	if err := store.SetBucketVersioning("b", storage.VersioningEnabled); err != nil {
		t.Fatalf("versioning: %v", err)
	}
	if _, err := store.RecordVersion("b", "k", "hash", 3); err != nil {
		t.Fatalf("record: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := ft.sentCount(); got != 0 {
		t.Fatalf("single-node setup must not replicate, got %d sends", got)
	}
}

func TestInstallVersionReplication_FansOutToPlacement(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ft := &recordingP2PTransport{}
	nodes := []*common.Node{{ID: 0, Weight: 1}, {ID: 1, Weight: 1}, {ID: 2, Weight: 1}}
	provider := p2p.NewMetadataRPCProvider(0, ft, p2p.NewRing([]int32{0, 1, 2}, 2), store)
	provider.RPCTimeout = 150 * time.Millisecond
	installVersionReplication(context.Background(), store, &common.ClusterMap{Nodes: nodes}, provider, 0, 2)

	if err := store.SetBucketVersioning("b", storage.VersioningEnabled); err != nil {
		t.Fatalf("versioning: %v", err)
	}
	if _, err := store.RecordVersion("b", "k", "hash-x", 3); err != nil {
		t.Fatalf("record: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for ft.sentCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.sent) != 1 {
		t.Fatalf("rf=2 must send to the one non-self replica, got %d", len(ft.sent))
	}
	args, derr := p2p.DecodeReplicateVersionArgs(ft.sent[0].Payload)
	if derr != nil {
		t.Fatalf("decode args: %v", derr)
	}
	if args.Bucket != "b" || args.Key != "k" || args.Record.VersionID == "" || args.VersioningStatus != storage.VersioningEnabled {
		t.Fatalf("unexpected replication payload: %+v", args)
	}
	if ft.sent[0].Type != p2p.MsgReplicateVersion {
		t.Fatalf("unexpected message type: %d", ft.sent[0].Type)
	}
}
