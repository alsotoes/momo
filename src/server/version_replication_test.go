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

func TestVersionReplicator_ErrorsAreNonFatal(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ft := &recordingP2PTransport{}
	provider := p2p.NewMetadataRPCProvider(0, ft, p2p.NewRing([]int32{0, 1}, 1), store)
	rec := storage.VersionRecord{VersionID: "v1", Hash: "hash", IsLatest: true}

	// Placement failure: a cluster map with no nodes.
	rep := &versionReplicator{store: store, cmap: &common.ClusterMap{}, provider: provider, self: 0, rf: 2}
	rep.fanOut(context.Background(), "b", "k", rec)
	if ft.sentCount() != 0 {
		t.Fatalf("placement failure must not send, got %d", ft.sentCount())
	}

	// Versioning read failure: closed store.
	closed, cerr := storage.NewCASStore(t.TempDir())
	if cerr != nil {
		t.Fatalf("store: %v", cerr)
	}
	_ = closed.Close()
	rep = &versionReplicator{store: closed, cmap: &common.ClusterMap{Nodes: []*common.Node{{ID: 0, Weight: 1}}}, provider: provider, self: 0, rf: 2}
	rep.fanOut(context.Background(), "b", "k", rec)
	if ft.sentCount() != 0 {
		t.Fatalf("versioning failure must not send, got %d", ft.sentCount())
	}
}

func TestWireVersionReplication(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ft := &recordingP2PTransport{}
	transport := p2p.NewTCPTransport(p2p.TCPTransportConfig{LocalID: 0, AuthFunc: func(int32) bool { return true }})
	_ = transport.Close()
	gossip := p2p.NewGossiper(p2p.GossipConfig{LocalID: 0}, ft)
	cfg := common.Configuration{}
	cfg.Global.ReplicationFactor = 2
	daemons := []*common.Daemon{{Host: "127.0.0.1:4440"}, {Host: "127.0.0.1:4441"}}
	wireVersionReplication(context.Background(), cfg, daemons, 0, store, ft, gossip)
	// The hook is installed: a local version fans out to the peer.
	if err := store.SetBucketVersioning("b", storage.VersioningEnabled); err != nil {
		t.Fatalf("versioning: %v", err)
	}
	if _, err := store.RecordVersion("b", "k", "hash", 3); err != nil {
		t.Fatalf("record: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for ft.sentCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if ft.sentCount() == 0 {
		t.Fatal("expected a replication send after wiring")
	}
}

func TestWireVersionReplication_SkipsNonCASStore(t *testing.T) {
	ft := &recordingP2PTransport{}
	gossip := p2p.NewGossiper(p2p.GossipConfig{LocalID: 0}, ft)
	// Must be a no-op rather than a panic when the store is not a CAS store.
	wireVersionReplication(context.Background(), common.Configuration{}, nil, 0, nil, ft, gossip)
}

func TestVersionPlacementKey(t *testing.T) {
	if got := versionPlacementKey("b", "k", storage.VersionRecord{Hash: "abc"}); got != "abc" {
		t.Fatalf("data versions key on the blob hash, got %q", got)
	}
	if got := versionPlacementKey("b", "k", storage.VersionRecord{IsDeleteMarker: true}); got != "b/k" {
		t.Fatalf("delete markers key on the object name, got %q", got)
	}
}
