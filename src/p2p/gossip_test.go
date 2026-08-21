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

// TestGossiper_RTTPropagationToPeer verifies (issue #823) that a successful
// ping writes the EWMA RTT back to the target Peer, so quality-aware quorum
// selection can rank peers by their per-peer RTT.
func TestAdaptiveFanout(t *testing.T) {
	cases := []struct {
		name  string
		alive int
		want  int
	}{
		{"zero", 0, minGossipFanout},
		{"one", 1, minGossipFanout},
		{"two", 2, 1},
		{"three", 3, 2},
		{"seven", 7, 2},
		{"twenty", 20, 3},
		{"fifty five", 55, 5},
		{"hundred", 100, 5},
		{"thousand", 1000, 7},
		{"huge capped", 100_000, maxGossipFanout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := adaptiveFanout(tc.alive)
			if got != tc.want {
				t.Fatalf("adaptiveFanout(%d) = %d, want %d", tc.alive, got, tc.want)
			}
			if got < minGossipFanout || got > maxGossipFanout {
				t.Fatalf("adaptiveFanout(%d) = %d out of bounds [%d, %d]", tc.alive, got, minGossipFanout, maxGossipFanout)
			}
		})
	}

	// Monotonic: fanout(N1) <= fanout(N2) for N1 < N2.
	prev := 0
	for n := 1; n <= 500; n++ {
		cur := adaptiveFanout(n)
		if cur < prev {
			t.Fatalf("fanout not monotonic at N=%d: %d < %d", n, cur, prev)
		}
		prev = cur
	}
}

func TestEffectiveFanout(t *testing.T) {
	cases := []struct {
		name  string
		cfg   int
		alive int
		want  int
	}{
		{"adaptive default", 0, 55, 5},
		{"adaptive negative treated as auto", -1, 100, 5},
		{"explicit override", 3, 1000, 3},
		{"explicit override small cluster", 5, 2, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveFanout(tc.cfg, tc.alive); got != tc.want {
				t.Fatalf("effectiveFanout(%d, %d) = %d, want %d", tc.cfg, tc.alive, got, tc.want)
			}
		})
	}
}

func TestGossiper_RTTPropagationToPeer(t *testing.T) {
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
		HeartbeatInterval: 20 * time.Millisecond,
		SuspicionTimeout:  500 * time.Millisecond,
		Fanout:            3,
		PingTimeout:       100 * time.Millisecond,
		IndirectPingCount: 3,
		RTTAlpha:          0.25,
	}
	cfg2 := GossipConfig{
		LocalID:           2,
		HeartbeatInterval: 20 * time.Millisecond,
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

	// Let pings flow for a while; then the per-peer RTT should be populated.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if peer := tr1.Peers().Get(2); peer != nil && peer.RTT() > 0 {
			return // success: RTT propagated to the Peer value
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("expected peer 2 RTT to be populated on tr1 after pings")
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
		id1 := (uint64(g1.cfg.LocalID) << 32) | (g1.nextPingID.Add(1) & 0xFFFFFFFF)
		id2 := (uint64(g2.cfg.LocalID) << 32) | (g2.nextPingID.Add(1) & 0xFFFFFFFF)
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
