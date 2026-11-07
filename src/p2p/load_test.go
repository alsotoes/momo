package p2p

import (
	"sync"
	"testing"
)

// loadTransport is a Transport stub with a real PeerMap so the gossiper can
// address peers and store their reports.
type loadTransport struct {
	mu   sync.Mutex
	sent []RPC
	pm   *PeerMap
}

func newLoadTransport(ids ...int32) *loadTransport {
	pm := NewPeerMap()
	for _, id := range ids {
		pm.Add(NewPeer(id, "127.0.0.1:0"))
	}
	return &loadTransport{pm: pm}
}

func (t *loadTransport) Listen(string) error               { return nil }
func (t *loadTransport) Dial(int32, string) (*Peer, error) { return nil, nil }
func (t *loadTransport) Connect(*Peer) error               { return nil }
func (t *loadTransport) Consume() <-chan RPC               { return nil }
func (t *loadTransport) Broadcast(*RPC) int                { return 0 }
func (t *loadTransport) Peers() *PeerMap                   { return t.pm }
func (t *loadTransport) Close() error                      { return nil }
func (t *loadTransport) Addr() string                      { return "" }

func (t *loadTransport) Send(_ int32, rpc *RPC) error {
	t.mu.Lock()
	t.sent = append(t.sent, *rpc)
	t.mu.Unlock()
	return nil
}

func (t *loadTransport) sentOfType(mt MessageType) []RPC {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []RPC
	for _, rpc := range t.sent {
		if rpc.Type == mt {
			out = append(out, rpc)
		}
	}
	return out
}

func TestLoadPayloadRoundTrip(t *testing.T) {
	in := &PeerLoad{DiskUsedPercent: 87, InflightRequests: 42, ReplicationQueue: 7, UpdatedUnixNano: 123456789}
	out, err := DecodeLoadPayload(in.Encode())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if *out != *in {
		t.Fatalf("round trip mismatch: %+v vs %+v", out, in)
	}
	if len(in.Encode()) != loadPayloadSize {
		t.Fatalf("wire size = %d, want %d", len(in.Encode()), loadPayloadSize)
	}
}

func TestDecodeLoadPayloadRejectsBadInput(t *testing.T) {
	tooShort := []byte{1, 2, 3}
	if _, err := DecodeLoadPayload(tooShort); err == nil {
		t.Fatal("short payload must be rejected")
	}
	tooBig := make([]byte, maxPayloadSize+1)
	if _, err := DecodeLoadPayload(tooBig); err == nil {
		t.Fatal("oversized payload must be rejected")
	}
	outOfRange := make([]byte, loadPayloadSize)
	outOfRange[0] = 101
	if _, err := DecodeLoadPayload(outOfRange); err == nil {
		t.Fatal("disk percent above 100 must be rejected")
	}
}

func TestGossiperPublishesLoadOnHeartbeat(t *testing.T) {
	tr := newLoadTransport(1)
	g := NewGossiper(GossipConfig{LocalID: 0, Fanout: 1}, tr)
	g.SetLoadProvider(func() PeerLoad {
		return PeerLoad{DiskUsedPercent: 55, InflightRequests: 3, UpdatedUnixNano: 99}
	})

	g.sendHeartbeat()

	loads := tr.sentOfType(MsgLoad)
	if len(loads) != 1 {
		t.Fatalf("expected one load broadcast, got %d", len(loads))
	}
	if loads[0].From != 0 {
		t.Fatalf("load must be attributed to the sender, got %d", loads[0].From)
	}
	decoded, err := DecodeLoadPayload(loads[0].Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.DiskUsedPercent != 55 || decoded.InflightRequests != 3 {
		t.Fatalf("unexpected payload: %+v", decoded)
	}
}

func TestGossiperWithoutProviderSendsNoLoad(t *testing.T) {
	tr := newLoadTransport(1)
	g := NewGossiper(GossipConfig{LocalID: 0, Fanout: 1}, tr)
	g.sendHeartbeat()
	if got := len(tr.sentOfType(MsgLoad)); got != 0 {
		t.Fatalf("no provider must mean no load messages, got %d", got)
	}
}

func TestGossiperRecordsPeerLoad(t *testing.T) {
	tr := newLoadTransport(1, 2)
	g := NewGossiper(GossipConfig{LocalID: 0}, tr)

	if _, ok := g.PeerLoad(1); ok {
		t.Fatal("no report yet")
	}

	report := &PeerLoad{DiskUsedPercent: 91, InflightRequests: 12, ReplicationQueue: 4, UpdatedUnixNano: 7}
	g.HandleRPC(&RPC{From: 1, Type: MsgLoad, Payload: report.Encode()})

	got, ok := g.PeerLoad(1)
	if !ok {
		t.Fatal("report not recorded")
	}
	if *report != got {
		t.Fatalf("stored %+v, want %+v", got, *report)
	}
	if _, ok := g.PeerLoad(99); ok {
		t.Fatal("unknown peer must not report load")
	}
}

func TestGossiperIgnoresMalformedLoad(t *testing.T) {
	tr := newLoadTransport(1)
	g := NewGossiper(GossipConfig{LocalID: 0}, tr)
	g.HandleRPC(&RPC{From: 1, Type: MsgLoad, Payload: []byte{1}})
	if _, ok := g.PeerLoad(1); ok {
		t.Fatal("malformed report must be dropped")
	}
	bad := make([]byte, loadPayloadSize)
	bad[0] = 200
	g.HandleRPC(&RPC{From: 1, Type: MsgLoad, Payload: bad})
	if _, ok := g.PeerLoad(1); ok {
		t.Fatal("out-of-range report must be dropped")
	}
}
