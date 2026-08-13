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

func TestMajorityQuorum(t *testing.T) {
	tests := []struct {
		peers int
		want  int
	}{
		{0, 0},  // no peers: no quorum possible (also guards split-brain)
		{1, 1},  // 1 of 1
		{2, 2},  // 2 of 2
		{3, 2},  // 2 of 3 (majority, not supermajority)
		{4, 3},  // 3 of 4
		{5, 3},  // 3 of 5 (majority, not supermajority)
		{9, 5},  // 5 of 9
		{10, 6}, // 6 of 10
	}
	for _, tc := range tests {
		if got := majorityQuorum(tc.peers); got != tc.want {
			t.Errorf("majorityQuorum(%d) = %d, want %d", tc.peers, got, tc.want)
		}
	}
}

// TestLeaseManager_PartitionGuard verifies that a lease is NOT granted when the
// cluster has configured peers but none are reachable (network partition). This
// prevents split-brain: each partition would otherwise grant the same key with a
// zero-quorum lease (issue #630).
func TestLeaseManager_PartitionGuard(t *testing.T) {
	tr := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	defer tr.Close()

	// Configure a peer in the map that is NOT alive, simulating a peer that was
	// once connected but is now unreachable (network partition).
	offlinePeer := NewPeer(2, "127.0.0.1:4451")
	offlinePeer.SetState(PeerStateOffline)
	tr.Peers().Add(offlinePeer)

	lm := NewLeaseManager(1, tr)
	lm.Start()
	defer lm.Stop()

	_, err := lm.Acquire("test-key", 1*time.Second)
	if err == nil {
		t.Fatal("expected lease acquisition to fail when cluster is partitioned (no reachable peers)")
	}
}

func TestLeaseManager_NoPeers(t *testing.T) {
	tr := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	defer tr.Close()

	lm := NewLeaseManager(1, tr)
	lm.Start()
	defer lm.Stop()

	lease, err := lm.Acquire("test-key", 5*time.Second)
	if err != nil {
		t.Fatalf("expected self-grant for single-node cluster, got error: %v", err)
	}
	if lease == nil {
		t.Fatal("expected non-nil lease for single-node cluster")
	}
	if lease.Key != "test-key" {
		t.Errorf("key mismatch: got %q, want %q", lease.Key, "test-key")
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
		t.Error("expected timeout error when remote peer does not grant lease")
	}
}

func TestLeaseManager_TwoNodeCluster(t *testing.T) {
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
	tr2.Dial(1, addr1)
	time.Sleep(100 * time.Millisecond)

	lm1 := NewLeaseManager(1, tr1)
	lm2 := NewLeaseManager(2, tr2)

	g1 := NewGossiper(DefaultGossipConfig(1), tr1)
	g2 := NewGossiper(DefaultGossipConfig(2), tr2)
	g1.SetLeaseManager(lm1)
	g2.SetLeaseManager(lm2)
	defer g1.Close()
	defer g2.Close()

	lm1.Start()
	lm2.Start()
	defer lm1.Stop()
	defer lm2.Stop()

	g1.Run()
	g2.Run()

	time.Sleep(200 * time.Millisecond)

	lease, err := lm1.Acquire("test-key", 5*time.Second)
	if err != nil {
		t.Fatalf("expected lease acquisition in 2-node cluster, got error: %v", err)
	}
	if lease == nil {
		t.Fatal("expected non-nil lease")
	}
	if lease.QuorumSize < 1 {
		t.Errorf("expected quorum >= 1, got %d", lease.QuorumSize)
	}

	if err := lm1.Release(lease); err != nil {
		t.Fatalf("release failed: %v", err)
	}
}
