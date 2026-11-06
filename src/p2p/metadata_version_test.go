package p2p

import (
	"errors"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/storage"
)

// fakeVersionTransport records sends and optionally injects a reply through the
// handler, standing in for a live peer.
type fakeVersionTransport struct {
	mu      sync.Mutex
	sent    []RPC
	onSend  func(rpc *RPC)
	sendErr error
}

func (f *fakeVersionTransport) Listen(string) error               { return nil }
func (f *fakeVersionTransport) Dial(int32, string) (*Peer, error) { return nil, nil }
func (f *fakeVersionTransport) Connect(*Peer) error               { return nil }
func (f *fakeVersionTransport) Consume() <-chan RPC               { return nil }
func (f *fakeVersionTransport) Broadcast(*RPC) int                { return 0 }
func (f *fakeVersionTransport) Peers() *PeerMap                   { return NewPeerMap() }
func (f *fakeVersionTransport) Close() error                      { return nil }
func (f *fakeVersionTransport) Addr() string                      { return "" }

func (f *fakeVersionTransport) Send(_ int32, rpc *RPC) error {
	f.mu.Lock()
	f.sent = append(f.sent, *rpc)
	onSend := f.onSend
	err := f.sendErr
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if onSend != nil {
		onSend(rpc)
	}
	return nil
}

func newVersionTestProvider(t *testing.T) (*MetadataRPCProvider, *storage.CASStore, *fakeVersionTransport) {
	t.Helper()
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ft := &fakeVersionTransport{}
	p := NewMetadataRPCProvider(1, ft, NewRing([]int32{0, 1, 2}, 2), store)
	return p, store, ft
}

func TestReplicateVersionPayloadRoundTrip(t *testing.T) {
	args := &ReplicateVersionArgs{
		RequestID:        42,
		Bucket:           "b",
		Key:              "k",
		Record:           storage.VersionRecord{VersionID: "v1", Hash: "h", Size: 3, Timestamp: 9, IsLatest: true},
		VersioningStatus: storage.VersioningEnabled,
	}
	payload, err := EncodeReplicateVersionArgs(args)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeReplicateVersionArgs(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.RequestID != args.RequestID || got.Bucket != args.Bucket || got.Record.VersionID != "v1" || got.VersioningStatus != storage.VersioningEnabled {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	replyPayload, err := EncodeReplicateVersionReply(&ReplicateVersionReply{RequestID: 42, Success: true})
	if err != nil {
		t.Fatalf("encode reply: %v", err)
	}
	reply, err := DecodeReplicateVersionReply(replyPayload)
	if err != nil || !reply.Success || reply.RequestID != 42 {
		t.Fatalf("reply round trip: %+v err=%v", reply, err)
	}
}

func TestHandleReplicateVersionApplies(t *testing.T) {
	p, store, ft := newVersionTestProvider(t)
	payload, _ := EncodeReplicateVersionArgs(&ReplicateVersionArgs{
		RequestID:        7,
		Bucket:           "b",
		Key:              "k",
		Record:           storage.VersionRecord{VersionID: "v1", Hash: "h", Size: 3, Timestamp: 10, IsLatest: true},
		VersioningStatus: storage.VersioningEnabled,
	})
	p.handleReplicateVersion(&RPC{From: 2, Type: MsgReplicateVersion, Payload: payload})

	status, configured, err := store.BucketVersioning("b")
	if err != nil || !configured || status != storage.VersioningEnabled {
		t.Fatalf("bucket versioning not replicated: %q configured=%v err=%v", status, configured, err)
	}
	rec, err := store.GetVersion("b", "k", "v1")
	if err != nil || rec.Hash != "h" {
		t.Fatalf("version record not replicated: %+v err=%v", rec, err)
	}
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.sent) != 1 || ft.sent[0].Type != MsgReplicateVersionResponse {
		t.Fatalf("expected one response, got %+v", ft.sent)
	}
}

func TestHandleReplicateVersionBadPayload(t *testing.T) {
	p, _, ft := newVersionTestProvider(t)
	p.handleReplicateVersion(&RPC{From: 2, Type: MsgReplicateVersion, Payload: []byte("not json")})
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.sent) != 0 {
		t.Fatalf("malformed payload must not be answered: %+v", ft.sent)
	}
}

func TestReplicateVersionWaitsForAck(t *testing.T) {
	p, _, ft := newVersionTestProvider(t)
	p.RPCTimeout = 2 * time.Second
	ft.onSend = func(rpc *RPC) {
		args, err := DecodeReplicateVersionArgs(rpc.Payload)
		if err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		reply, _ := EncodeReplicateVersionReply(&ReplicateVersionReply{RequestID: args.RequestID, Success: true})
		p.handleReplicateVersionResponse(&RPC{From: 2, Type: MsgReplicateVersionResponse, Payload: reply})
	}
	if _, err := p.ReplicateVersion(2, &ReplicateVersionArgs{Bucket: "b", Key: "k"}); err != nil {
		t.Fatalf("ReplicateVersion: %v", err)
	}
}

func TestReplicateVersionErrors(t *testing.T) {
	p, _, ft := newVersionTestProvider(t)
	p.RPCTimeout = 50 * time.Millisecond
	ft.sendErr = errors.New("boom")
	if _, err := p.ReplicateVersion(2, &ReplicateVersionArgs{Bucket: "b", Key: "k"}); err == nil {
		t.Fatal("expected send error")
	}

	ft.sendErr = nil
	ft.onSend = nil
	_, err := p.ReplicateVersion(2, &ReplicateVersionArgs{Bucket: "b", Key: "k"})
	if !errors.Is(err, syscall.ETIMEDOUT) {
		t.Fatalf("want ETIMEDOUT, got %v", err)
	}

	ft.onSend = func(rpc *RPC) {
		args, _ := DecodeReplicateVersionArgs(rpc.Payload)
		reply, _ := EncodeReplicateVersionReply(&ReplicateVersionReply{RequestID: args.RequestID, Success: false, Error: "denied"})
		p.handleReplicateVersionResponse(&RPC{From: 2, Type: MsgReplicateVersionResponse, Payload: reply})
	}
	if _, err := p.ReplicateVersion(2, &ReplicateVersionArgs{Bucket: "b", Key: "k"}); !errors.Is(err, syscall.EIO) {
		t.Fatalf("want EIO on rejection, got %v", err)
	}
}

func TestRPCTimeoutFallback(t *testing.T) {
	p := &MetadataRPCProvider{}
	if p.rpcTimeout() != MetadataRPCTimeout {
		t.Fatalf("zero RPCTimeout must fall back to the constant")
	}
	p.RPCTimeout = time.Millisecond
	if p.rpcTimeout() != time.Millisecond {
		t.Fatalf("explicit RPCTimeout must win")
	}
}

func TestReplicateVersionResponseUnknownRequest(t *testing.T) {
	p, _, _ := newVersionTestProvider(t)
	payload, _ := EncodeReplicateVersionReply(&ReplicateVersionReply{RequestID: 999, Success: true})
	p.handleReplicateVersionResponse(&RPC{From: 2, Type: MsgReplicateVersionResponse, Payload: payload})
	p.handleReplicateVersionResponse(&RPC{From: 2, Type: MsgReplicateVersionResponse, Payload: []byte("bad")})
}
