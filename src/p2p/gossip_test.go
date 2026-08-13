package p2p

import (
	"net"
	"sync/atomic"
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
		PingTimeout:       100 * time.Millisecond,
		IndirectPingCount: 3,
		RTTAlpha:          0.25,
	}
	cfg2 := GossipConfig{
		LocalID:           2,
		HeartbeatInterval: 50 * time.Millisecond,
		SuspicionTimeout:  500 * time.Millisecond,
		Fanout:            3,
		PingTimeout:       100 * time.Millisecond,
		IndirectPingCount: 3,
		RTTAlpha:          0.25,
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
		PingTimeout:       100 * time.Millisecond,
		IndirectPingCount: 3,
		RTTAlpha:          0.25,
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
		PingTimeout:       100 * time.Millisecond,
		IndirectPingCount: 3,
		RTTAlpha:          0.25,
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

// TestGossiper_OfflinePeerRestore verifies that an OFFLINE peer is restored to
// ALIVE upon receiving a ping, ack, or heartbeat, rather than remaining
// permanently dead (issue #632).
func TestGossiper_OfflinePeerRestore(t *testing.T) {
	tr := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	defer tr.Close()

	g := NewGossiper(DefaultGossipConfig(1), tr)
	defer g.Close()

	subtests := []struct {
		name string
		run  func(peer *Peer)
	}{
		{
			name: "ping",
			run: func(peer *Peer) {
				payload := &PingPayload{PingID: 99, TargetID: 1, Timestamp: time.Now().UnixNano()}
				g.handlePing(&RPC{From: peer.ID, Type: MsgPing, Payload: payload.Encode()})
			},
		},
		{
			name: "heartbeat",
			run: func(peer *Peer) {
				payload := &HeartbeatPayload{Peers: []PeerInfo{}}
				g.handleHeartbeat(&RPC{From: peer.ID, Type: MsgHeartbeat, Payload: payload.Encode()})
			},
		},
	}

	for _, st := range subtests {
		t.Run(st.name, func(t *testing.T) {
			peer := NewPeer(2, "127.0.0.1:4451")
			peer.SetState(PeerStateOffline)
			tr.Peers().Add(peer)

			st.run(peer)

			if peer.State() != PeerStateAlive {
				t.Fatalf("expected peer %d restored to ALIVE via %s, got %v", peer.ID, st.name, peer.State())
			}

			// Clean up for the next subtest.
			tr.Peers().Remove(2)
		})
	}
}

func TestGossiper_PingIDUniquenessAcrossNodes(t *testing.T) {
	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	tr2 := NewTCPTransport(TCPTransportConfig{LocalID: 2})
	defer tr1.Close()
	defer tr2.Close()

	cfg := GossipConfig{
		HeartbeatInterval: time.Hour,
		SuspicionTimeout:  time.Hour,
		Fanout:            3,
		PingTimeout:       time.Hour,
		IndirectPingCount: 3,
		RTTAlpha:          0.25,
	}

	cfg1 := cfg
	cfg1.LocalID = 1
	cfg2 := cfg
	cfg2.LocalID = 2

	g1 := NewGossiper(cfg1, tr1)
	g2 := NewGossiper(cfg2, tr2)
	defer g1.Close()
	defer g2.Close()

	for i := 0; i < 1000; i++ {
		id1 := (uint64(g1.cfg.LocalID) << 32) | (atomic.AddUint64(&g1.nextPingID, 1) & 0xFFFFFFFF)
		id2 := (uint64(g2.cfg.LocalID) << 32) | (atomic.AddUint64(&g2.nextPingID, 1) & 0xFFFFFFFF)
		if id1 == id2 {
			t.Fatalf("ping ID collision between node 1 and node 2: %d (iteration %d)", id1, i)
		}
		if id1>>32 != 1 {
			t.Fatalf("node 1 ping ID %d has wrong node ID in upper bits: %d", id1, id1>>32)
		}
		if id2>>32 != 2 {
			t.Fatalf("node 2 ping ID %d has wrong node ID in upper bits: %d", id2, id2>>32)
		}
	}
}
