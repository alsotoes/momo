package p2p

import (
	"net"
	"testing"
	"time"
)

func TestIntegration_ThreeNodeCluster(t *testing.T) {
	transports := make([]*TCPTransport, 3)
	gossipers := make([]*Gossiper, 3)
	addrs := make([]string, 3)

	for i := 0; i < 3; i++ {
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		addrs[i] = ln.Addr().String()
		ln.Close()

		transports[i] = NewTCPTransport(TCPTransportConfig{LocalID: int32(i)})
		if err := transports[i].Listen(addrs[i]); err != nil {
			t.Fatalf("node %d listen failed: %v", i, err)
		}

		cfg := GossipConfig{
			LocalID:           int32(i),
			HeartbeatInterval: 50 * time.Millisecond,
			SuspicionTimeout:  300 * time.Millisecond,
			Fanout:            3,
		}
		gossipers[i] = NewGossiper(cfg, transports[i])
	}

	defer func() {
		for i := 0; i < 3; i++ {
			gossipers[i].Close()
			transports[i].Close()
		}
	}()

	transports[0].Dial(1, addrs[1])
	transports[0].Dial(2, addrs[2])

	for i := 0; i < 3; i++ {
		gossipers[i].Run()
	}

	time.Sleep(1 * time.Second)

	for i := 0; i < 3; i++ {
		count := transports[i].Peers().Count()
		if count < 2 {
			t.Errorf("node %d expected at least 2 peers, got %d", i, count)
		}
	}
}

func TestIntegration_NodeJoinAfterStart(t *testing.T) {
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

	cfg := GossipConfig{
		HeartbeatInterval: 50 * time.Millisecond,
		SuspicionTimeout:  300 * time.Millisecond,
		Fanout:            3,
	}
	cfg1 := cfg
	cfg1.LocalID = 1
	cfg2 := cfg
	cfg2.LocalID = 2

	g1 := NewGossiper(cfg1, tr1)
	g2 := NewGossiper(cfg2, tr2)
	defer g1.Close()
	defer g2.Close()
	defer tr1.Close()
	defer tr2.Close()

	g1.Run()
	g2.Run()

	time.Sleep(300 * time.Millisecond)

	tr3 := NewTCPTransport(TCPTransportConfig{LocalID: 3})
	defer tr3.Close()

	ln3, _ := net.Listen("tcp", "127.0.0.1:0")
	addr3 := ln3.Addr().String()
	ln3.Close()

	tr3.Listen(addr3)
	tr3.Dial(1, addr1)

	cfg3 := cfg
	cfg3.LocalID = 3
	g3 := NewGossiper(cfg3, tr3)
	defer g3.Close()
	g3.Run()

	time.Sleep(500 * time.Millisecond)

	if tr1.Peers().Get(3) == nil {
		t.Error("node 1 should have discovered node 3 via gossip")
	}
	if tr2.Peers().Get(3) == nil {
		t.Error("node 2 should have discovered node 3 via gossip dissemination")
	}
}
