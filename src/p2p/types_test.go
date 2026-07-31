package p2p

import (
	"bytes"
	"testing"
)

func TestRPC_EncodeDecode(t *testing.T) {
	original := &RPC{
		From:    42,
		Type:    MsgHeartbeat,
		Payload: []byte("hello world"),
	}

	encoded := original.Encode()

	if len(encoded) != 4+1+4+len("hello world") {
		t.Errorf("unexpected encoded length %d", len(encoded))
	}

	r := bytes.NewReader(encoded)
	decoded, err := DecodeRPC(r)
	if err != nil {
		t.Fatalf("DecodeRPC failed: %v", err)
	}

	if decoded.From != original.From {
		t.Errorf("From mismatch: got %d, want %d", decoded.From, original.From)
	}
	if decoded.Type != original.Type {
		t.Errorf("Type mismatch: got %d, want %d", decoded.Type, original.Type)
	}
	if !bytes.Equal(decoded.Payload, original.Payload) {
		t.Errorf("Payload mismatch: got %q, want %q", decoded.Payload, original.Payload)
	}
}

func TestRPC_EmptyPayload(t *testing.T) {
	original := &RPC{
		From:    1,
		Type:    MsgSuspect,
		Payload: nil,
	}

	encoded := original.Encode()
	r := bytes.NewReader(encoded)
	decoded, err := DecodeRPC(r)
	if err != nil {
		t.Fatalf("DecodeRPC failed: %v", err)
	}
	if len(decoded.Payload) != 0 {
		t.Errorf("expected empty payload, got %d bytes", len(decoded.Payload))
	}
}

func TestHeartbeatPayload_EncodeDecode(t *testing.T) {
	original := &HeartbeatPayload{
		Peers: []PeerInfo{
			{ID: 1, Addr: "127.0.0.1:4450"},
			{ID: 2, Addr: "127.0.0.2:4450"},
			{ID: 3, Addr: "10.0.0.1:4450"},
		},
	}

	encoded := original.Encode()
	decoded, err := DecodeHeartbeatPayload(encoded)
	if err != nil {
		t.Fatalf("DecodeHeartbeatPayload failed: %v", err)
	}

	if len(decoded.Peers) != len(original.Peers) {
		t.Fatalf("peer count mismatch: got %d, want %d", len(decoded.Peers), len(original.Peers))
	}

	for i, p := range original.Peers {
		if decoded.Peers[i].ID != p.ID {
			t.Errorf("peer %d ID mismatch: got %d, want %d", i, decoded.Peers[i].ID, p.ID)
		}
		if decoded.Peers[i].Addr != p.Addr {
			t.Errorf("peer %d Addr mismatch: got %q, want %q", i, decoded.Peers[i].Addr, p.Addr)
		}
	}
}

func TestHeartbeatPayload_Empty(t *testing.T) {
	original := &HeartbeatPayload{Peers: nil}
	encoded := original.Encode()
	decoded, err := DecodeHeartbeatPayload(encoded)
	if err != nil {
		t.Fatalf("DecodeHeartbeatPayload failed: %v", err)
	}
	if len(decoded.Peers) != 0 {
		t.Errorf("expected 0 peers, got %d", len(decoded.Peers))
	}
}

func TestPeer_StateTransitions(t *testing.T) {
	p := NewPeer(1, "127.0.0.1:4450")

	if p.State() != PeerStateAlive {
		t.Errorf("expected PeerStateAlive, got %d", p.State())
	}

	p.SetState(PeerStateSuspect)
	if p.State() != PeerStateSuspect {
		t.Errorf("expected PeerStateSuspect, got %d", p.State())
	}

	p.SetState(PeerStateOffline)
	if p.State() != PeerStateOffline {
		t.Errorf("expected PeerStateOffline, got %d", p.State())
	}
}

func TestPeer_Touch(t *testing.T) {
	p := NewPeer(1, "127.0.0.1:4450")
	first := p.LastSeen()

	p.Touch()
	second := p.LastSeen()

	if !second.After(first) && !second.Equal(first) {
		t.Errorf("Touch did not update LastSeen: first=%v, second=%v", first, second)
	}
}
