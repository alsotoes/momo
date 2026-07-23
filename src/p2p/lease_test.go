package p2p

import (
	"net"
	"testing"
	"time"
)

func TestLeaseManager_AcquireRelease(t *testing.T) {
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
	time.Sleep(100 * time.Millisecond)

	lm1 := NewLeaseManager(1, tr1)
	lm2 := NewLeaseManager(2, tr2)
	lm3 := NewLeaseManager(3, tr3)

	g1 := NewGossiper(DefaultGossipConfig(1), tr1)
	g2 := NewGossiper(DefaultGossipConfig(2), tr2)
	g3 := NewGossiper(DefaultGossipConfig(3), tr3)
	g1.SetLeaseManager(lm1)
	g2.SetLeaseManager(lm2)
	g3.SetLeaseManager(lm3)
	defer g1.Close()
	defer g2.Close()
	defer g3.Close()

	lm1.Start()
	lm2.Start()
	lm3.Start()
	defer lm1.Stop()
	defer lm2.Stop()
	defer lm3.Stop()

	g1.Run()
	g2.Run()
	g3.Run()

	time.Sleep(200 * time.Millisecond)

	lease, err := lm1.Acquire("test-key", 5*time.Second)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if lease == nil {
		t.Fatal("expected non-nil lease")
	}
	if lease.Key != "test-key" {
		t.Errorf("key mismatch: got %q, want %q", lease.Key, "test-key")
	}
	if lease.QuorumSize < 2 {
		t.Errorf("expected quorum >= 2, got %d", lease.QuorumSize)
	}

	if err := lm1.Release(lease); err != nil {
		t.Fatalf("release failed: %v", err)
	}
}

func TestLeaseManager_NoPeers(t *testing.T) {
	tr := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	defer tr.Close()

	lm := NewLeaseManager(1, tr)
	lm.Start()
	defer lm.Stop()

	_, err := lm.Acquire("test-key", 5*time.Second)
	if err == nil {
		t.Error("expected error with no peers")
	}
}

func TestLeaseManager_Expiry(t *testing.T) {
	tr := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	defer tr.Close()

	lm := NewLeaseManager(1, tr)
	lm.Start()
	defer lm.Stop()

	lm.grantedMu.Lock()
	lm.granted["expiring-key"] = time.Now().Add(-1 * time.Hour).UnixNano()
	lm.grantedMu.Unlock()

	time.Sleep(700 * time.Millisecond)

	lm.grantedMu.Lock()
	_, exists := lm.granted["expiring-key"]
	lm.grantedMu.Unlock()

	if exists {
		t.Error("expected expired lease to be cleaned up")
	}
}

func TestLeaseManager_QuorumTimeout(t *testing.T) {
	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	defer tr1.Close()

	ln1, _ := net.Listen("tcp", "127.0.0.1:0")
	addr1 := ln1.Addr().String()
	ln1.Close()

	tr1.Listen(addr1)

	lm1 := NewLeaseManager(1, tr1)
	lm1.Start()
	defer lm1.Stop()

	g1 := NewGossiper(DefaultGossipConfig(1), tr1)
	g1.SetLeaseManager(lm1)
	defer g1.Close()
	g1.Run()

	time.Sleep(100 * time.Millisecond)

	_, err := lm1.Acquire("test-key", 1*time.Second)
	if err == nil {
		t.Error("expected timeout error with no peers for quorum")
	}
}
