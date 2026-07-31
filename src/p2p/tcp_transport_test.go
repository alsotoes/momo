package p2p

import (
	"net"
	"testing"
	"time"
)

func TestTCPTransport_ListenDial(t *testing.T) {
	tr := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	defer tr.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	if err := tr.Listen(addr); err != nil {
		t.Fatalf("Listen failed: %v", err)
	}

	if tr.Addr() == "" {
		t.Error("Addr() should return non-empty after Listen")
	}

	peer, err := tr.Dial(2, addr)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	if peer.ID != 2 {
		t.Errorf("expected peer ID 2, got %d", peer.ID)
	}

	if tr.Peers().Count() < 1 {
		t.Error("expected at least 1 peer after dial")
	}
}

func TestTCPTransport_SendReceive(t *testing.T) {
	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	tr2 := NewTCPTransport(TCPTransportConfig{LocalID: 2})
	defer tr1.Close()
	defer tr2.Close()

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr1 := ln.Addr().String()
	ln.Close()

	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	addr2 := ln2.Addr().String()
	ln2.Close()

	if err := tr1.Listen(addr1); err != nil {
		t.Fatalf("tr1 Listen failed: %v", err)
	}
	if err := tr2.Listen(addr2); err != nil {
		t.Fatalf("tr2 Listen failed: %v", err)
	}

	if _, err := tr1.Dial(2, addr2); err != nil {
		t.Fatalf("tr1 Dial failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	rpc := &RPC{
		From:    1,
		Type:    MsgHeartbeat,
		Payload: []byte("ping"),
	}

	if err := tr1.Send(2, rpc); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	select {
	case received := <-tr2.Consume():
		if received.From != 1 {
			t.Errorf("expected From=1, got %d", received.From)
		}
		if received.Type != MsgHeartbeat {
			t.Errorf("expected Type=MsgHeartbeat, got %d", received.Type)
		}
		if string(received.Payload) != "ping" {
			t.Errorf("expected payload 'ping', got %q", received.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for RPC")
	}
}

func TestTCPTransport_Broadcast(t *testing.T) {
	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	tr2 := NewTCPTransport(TCPTransportConfig{LocalID: 2})
	tr3 := NewTCPTransport(TCPTransportConfig{LocalID: 3})
	defer tr1.Close()
	defer tr2.Close()
	defer tr3.Close()

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr1 := ln.Addr().String()
	ln.Close()

	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	addr2 := ln2.Addr().String()
	ln2.Close()

	ln3, _ := net.Listen("tcp", "127.0.0.1:0")
	addr3 := ln3.Addr().String()
	ln3.Close()

	tr1.Listen(addr1)
	tr2.Listen(addr2)
	tr3.Listen(addr3)

	tr1.Dial(2, addr2)
	tr1.Dial(3, addr3)

	time.Sleep(100 * time.Millisecond)

	rpc := &RPC{
		From:    1,
		Type:    MsgHeartbeat,
		Payload: []byte("broadcast"),
	}

	sent := tr1.Broadcast(rpc)
	if sent < 2 {
		t.Errorf("expected at least 2 sends, got %d", sent)
	}
}
