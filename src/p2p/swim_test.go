package p2p

import (
	"net"
	"testing"
	"time"
)

func TestPingPayload_EncodeDecode(t *testing.T) {
	original := &PingPayload{
		PingID:    12345,
		TargetID:  42,
		Timestamp: time.Now().UnixNano(),
	}
	encoded := original.Encode()
	decoded, err := DecodePingPayload(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.PingID != original.PingID {
		t.Errorf("PingID mismatch: got %d, want %d", decoded.PingID, original.PingID)
	}
	if decoded.TargetID != original.TargetID {
		t.Errorf("TargetID mismatch: got %d, want %d", decoded.TargetID, original.TargetID)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp mismatch: got %d, want %d", decoded.Timestamp, original.Timestamp)
	}
}

func TestPingPayload_DecodeTooShort(t *testing.T) {
	_, err := DecodePingPayload([]byte{1, 2, 3})
	if err == nil {
		t.Error("expected error for short payload")
	}
}

func TestRTTTracker_UpdateAndGet(t *testing.T) {
	tracker := newRTTTracker(0.25)

	rtt1 := tracker.Update(1, 10*time.Millisecond)
	if rtt1 != 10*time.Millisecond {
		t.Errorf("first update should return sample, got %v", rtt1)
	}

	rtt2 := tracker.Update(1, 20*time.Millisecond)
	expected := time.Duration(float64(10*time.Millisecond)*(1-0.25) + float64(20*time.Millisecond)*0.25)
	if rtt2 != expected {
		t.Errorf("EWMA mismatch: got %v, want %v", rtt2, expected)
	}

	rtt3 := tracker.Update(1, 30*time.Millisecond)
	if rtt3 <= rtt2 {
		t.Error("EWMA should increase with higher sample")
	}

	if tracker.Get(2) != 0 {
		t.Error("unknown peer should have 0 RTT")
	}
}

func TestRTTTracker_EWMAConvergence(t *testing.T) {
	tracker := newRTTTracker(0.25)

	for i := 0; i < 100; i++ {
		tracker.Update(1, 50*time.Millisecond)
	}
	rtt := tracker.Get(1)
	if rtt < 49*time.Millisecond || rtt > 51*time.Millisecond {
		t.Errorf("EWMA should converge to 50ms, got %v", rtt)
	}
}

func TestAdaptiveTimeout_Fallback(t *testing.T) {
	cfg := DefaultGossipConfig(1)
	g := NewGossiper(cfg, nil)

	timeout := g.adaptiveTimeout(99)
	if timeout != cfg.SuspicionTimeout {
		t.Errorf("unknown peer should use default timeout, got %v, want %v", timeout, cfg.SuspicionTimeout)
	}
}

func TestAdaptiveTimeout_WithRTT(t *testing.T) {
	cfg := DefaultGossipConfig(1)
	cfg.SuspicionTimeout = 100 * time.Millisecond
	g := NewGossiper(cfg, nil)

	g.rtt.Update(1, 30*time.Millisecond)
	timeout := g.adaptiveTimeout(1)
	expected := 30 * time.Millisecond * 10
	if timeout != expected {
		t.Errorf("adaptive timeout should be 10x RTT, got %v, want %v", timeout, expected)
	}
}

func TestAdaptiveTimeout_CappedAtMin(t *testing.T) {
	cfg := DefaultGossipConfig(1)
	cfg.SuspicionTimeout = 5 * time.Second
	g := NewGossiper(cfg, nil)

	g.rtt.Update(1, 1*time.Millisecond)
	timeout := g.adaptiveTimeout(1)
	if timeout != cfg.SuspicionTimeout {
		t.Errorf("adaptive timeout should not go below SuspicionTimeout, got %v, want %v", timeout, cfg.SuspicionTimeout)
	}
}

func TestAdaptiveTimeout_CappedAtMax(t *testing.T) {
	cfg := DefaultGossipConfig(1)
	cfg.SuspicionTimeout = 1 * time.Second
	g := NewGossiper(cfg, nil)

	g.rtt.Update(1, 10*time.Second)
	timeout := g.adaptiveTimeout(1)
	maxTimeout := 5 * cfg.SuspicionTimeout
	if timeout != maxTimeout {
		t.Errorf("adaptive timeout should be capped at 5x SuspicionTimeout, got %v, want %v", timeout, maxTimeout)
	}
}

func TestGossiper_PingAck(t *testing.T) {
	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	tr2 := NewTCPTransport(TCPTransportConfig{LocalID: 2})
	defer tr1.Close()
	defer tr2.Close()

	ln1, _ := net.Listen("tcp", "127.0.0.1:0")
	addr1 := ln1.Addr().String()
	ln1.Close()
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	addr2 := ln2.Addr().String()
	ln2.Close()

	tr1.Listen(addr1)
	tr2.Listen(addr2)
	tr1.Dial(2, addr2)
	time.Sleep(100 * time.Millisecond)

	cfg1 := DefaultGossipConfig(1)
	cfg1.HeartbeatInterval = 50 * time.Millisecond
	cfg1.PingTimeout = 200 * time.Millisecond
	cfg2 := DefaultGossipConfig(2)
	cfg2.HeartbeatInterval = 50 * time.Millisecond
	cfg2.PingTimeout = 200 * time.Millisecond

	g1 := NewGossiper(cfg1, tr1)
	g2 := NewGossiper(cfg2, tr2)
	defer g1.Close()
	defer g2.Close()

	g1.Run()
	g2.Run()

	time.Sleep(500 * time.Millisecond)

	rtt := g1.rtt.Get(2)
	if rtt <= 0 {
		t.Error("RTT to peer 2 should be positive after ping/ack exchange")
	}
}

func TestGossiper_SuspicionRestoration(t *testing.T) {
	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	tr2 := NewTCPTransport(TCPTransportConfig{LocalID: 2})
	defer tr1.Close()
	defer tr2.Close()

	ln1, _ := net.Listen("tcp", "127.0.0.1:0")
	addr1 := ln1.Addr().String()
	ln1.Close()
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	addr2 := ln2.Addr().String()
	ln2.Close()

	tr1.Listen(addr1)
	tr2.Listen(addr2)
	tr1.Dial(2, addr2)
	time.Sleep(100 * time.Millisecond)

	cfg1 := DefaultGossipConfig(1)
	cfg1.HeartbeatInterval = 50 * time.Millisecond
	cfg1.SuspicionTimeout = 10 * time.Second
	cfg1.PingTimeout = 200 * time.Millisecond

	g1 := NewGossiper(cfg1, tr1)
	defer g1.Close()

	g1.Run()

	time.Sleep(200 * time.Millisecond)

	peer := tr1.Peers().Get(2)
	if peer == nil {
		t.Fatal("peer 2 should exist")
	}

	peer.SetState(PeerStateSuspect)
	if peer.State() != PeerStateSuspect {
		t.Fatalf("expected suspect state, got %d", peer.State())
	}

	cfg2 := DefaultGossipConfig(2)
	cfg2.HeartbeatInterval = 50 * time.Millisecond
	cfg2.PingTimeout = 200 * time.Millisecond
	g2 := NewGossiper(cfg2, tr2)
	defer g2.Close()
	g2.Run()

	time.Sleep(500 * time.Millisecond)

	if peer.State() != PeerStateAlive {
		t.Errorf("peer 2 should be restored to ALIVE, got state %d", peer.State())
	}
}

func TestGossiper_IndirectPing(t *testing.T) {
	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	tr2 := NewTCPTransport(TCPTransportConfig{LocalID: 2})
	tr3 := NewTCPTransport(TCPTransportConfig{LocalID: 3})
	defer tr1.Close()
	defer tr2.Close()
	defer tr3.Close()

	ln1, _ := net.Listen("tcp", "127.0.0.1:0")
	addr1 := ln1.Addr().String()
	ln1.Close()
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
	tr2.Dial(3, addr3)
	time.Sleep(100 * time.Millisecond)

	cfg := DefaultGossipConfig(1)
	cfg.HeartbeatInterval = 50 * time.Millisecond
	cfg.PingTimeout = 100 * time.Millisecond
	cfg.IndirectPingCount = 2

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

	if tr1.Peers().Get(2) == nil || tr1.Peers().Get(3) == nil {
		t.Error("node 1 should know about peers 2 and 3")
	}
	if tr2.Peers().Get(1) == nil || tr2.Peers().Get(3) == nil {
		t.Error("node 2 should know about peers 1 and 3")
	}
}

func TestGossiper_PingLoopPanicRecovery(t *testing.T) {
	cfg := DefaultGossipConfig(1)
	cfg.HeartbeatInterval = 10 * time.Millisecond
	cfg.PingTimeout = 5 * time.Millisecond

	tr := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	defer tr.Close()

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	tr.Listen(addr)

	g := NewGossiper(cfg, tr)
	g.Start()
	defer g.Close()

	time.Sleep(100 * time.Millisecond)
}
