package p2p

import (
	"net"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestGossiper_HeartbeatExchange(t *testing.T) {
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

	tr1.Listen(addr1)
	tr2.Listen(addr2)

	tr1.Dial(2, addr2)
	time.Sleep(100 * time.Millisecond)

	cfg1 := GossipConfig{
		LocalID:           1,
		HeartbeatInterval: 50 * time.Millisecond,
		SuspicionTimeout:  500 * time.Millisecond,
		Fanout:            3,
	}
	cfg2 := GossipConfig{
		LocalID:           2,
		HeartbeatInterval: 50 * time.Millisecond,
		SuspicionTimeout:  500 * time.Millisecond,
		Fanout:            3,
	}

	g1 := NewGossiper(cfg1, tr1)
	g2 := NewGossiper(cfg2, tr2)
	defer g1.Close()
	defer g2.Close()

	g1.Run()
	g2.Run()

	time.Sleep(300 * time.Millisecond)

	if tr1.Peers().Get(2) == nil {
		t.Error("peer 2 should be in tr1's peer map")
	}
	if tr2.Peers().Get(1) == nil {
		t.Error("peer 1 should be in tr2's peer map")
	}
}

func TestGossiper_MembershipDissemination(t *testing.T) {
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
	tr2.Dial(3, addr3)
	time.Sleep(100 * time.Millisecond)

	cfg := GossipConfig{
		HeartbeatInterval: 50 * time.Millisecond,
		SuspicionTimeout:  500 * time.Millisecond,
		Fanout:            3,
	}

	cfg1 := cfg
	cfg1.LocalID = 1
	cfg2 := cfg
	cfg2.LocalID = 2
	cfg3 := cfg
	cfg3.LocalID = 3

	g1 := NewGossiper(cfg1, tr1)
	g2 := NewGossiper(cfg2, tr2)
	g3 := NewGossiper(cfg3, tr3)
	defer g1.Close()
	defer g2.Close()
	defer g3.Close()

	g1.Run()
	g2.Run()
	g3.Run()

	time.Sleep(500 * time.Millisecond)

	if tr1.Peers().Get(3) == nil {
		t.Error("peer 3 should have been discovered by node 1 via gossip")
	}
}

func TestGossiper_SuspicionTimeout(t *testing.T) {
	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	tr2 := NewTCPTransport(TCPTransportConfig{LocalID: 2})

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr1 := ln.Addr().String()
	ln.Close()
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	addr2 := ln2.Addr().String()
	ln2.Close()

	tr1.Listen(addr1)
	tr2.Listen(addr2)

	tr1.Dial(2, addr2)
	time.Sleep(100 * time.Millisecond)

	cfg1 := GossipConfig{
		LocalID:           1,
		HeartbeatInterval: 50 * time.Millisecond,
		SuspicionTimeout:  150 * time.Millisecond,
		Fanout:            3,
	}

	g1 := NewGossiper(cfg1, tr1)
	defer g1.Close()
	defer tr1.Close()
	defer tr2.Close()

	g1.Run()

	time.Sleep(200 * time.Millisecond)

	peer := tr1.Peers().Get(2)
	if peer == nil {
		t.Fatal("peer 2 should exist before disconnect")
	}
	conn := peer.Conn()
	if conn != nil {
		conn.Close()
	}

	time.Sleep(500 * time.Millisecond)

	peer = tr1.Peers().Get(2)
	if peer == nil {
		return
	}
	if peer.State() != PeerStateSuspect && peer.State() != PeerStateOffline {
		t.Errorf("expected peer 2 to be suspect or offline, got state %d", peer.State())
	}
}
