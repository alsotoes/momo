package p2p

import (
	"errors"
	"syscall"
	"testing"
)

func TestClusterQuorumFromConfiguredSize(t *testing.T) {
	lm := NewLeaseManager(1, newFailoverTransport(1))
	if got := lm.ClusterQuorum(); got != 0 {
		t.Fatalf("unknown cluster size must report 0, got %d", got)
	}
	for size, want := range map[int]int{1: 1, 2: 2, 3: 2, 4: 3, 5: 3} {
		lm.SetClusterSize(size)
		if got := lm.ClusterQuorum(); got != want {
			t.Fatalf("size %d: quorum %d, want %d", size, got, want)
		}
	}
}

func TestHasQuorumRequiresMajorityOfCluster(t *testing.T) {
	tr := newFailoverTransport(1, 2)
	lm := NewLeaseManager(1, tr)

	// Unknown size: assume yes, preserving legacy behavior.
	if !lm.HasQuorum() {
		t.Fatal("unknown cluster size must not block coordination")
	}

	lm.SetClusterSize(5) // needs 3 of 5; self + one peer = 2
	if lm.HasQuorum() {
		t.Fatal("a minority partition must not have quorum")
	}

	tr.setAlive(3, true) // self + 2 peers = 3
	if !lm.HasQuorum() {
		t.Fatal("a majority partition must have quorum")
	}
}

func TestAcquireRefusesMinorityPartition(t *testing.T) {
	tr := newFailoverTransport(1, 2)
	lm := NewLeaseManager(1, tr)
	lm.SetClusterSize(5)

	_, err := lm.Acquire("metrics-controller", 5_000_000_000)
	if err == nil {
		t.Fatal("a minority partition must not grant a lease")
	}
	if !errors.Is(err, syscall.EHOSTUNREACH) {
		t.Fatalf("want EHOSTUNREACH, got %v", err)
	}
}

func TestOnPeerFailureSuspendsAllOnQuorumLoss(t *testing.T) {
	tr := newFailoverTransport(1, 2, 3, 4)
	lm := NewLeaseManager(1, tr)
	lm.SetClusterSize(5)
	// Two independent leases, both individually satisfiable.
	lm.held[1] = &Lease{LeaseID: 1, HolderID: 1, Key: "metrics-controller", QuorumSize: 0}
	lm.held[2] = &Lease{LeaseID: 2, HolderID: 1, Key: "replication-propagator", QuorumSize: 0}

	// Drop from 4 reachable peers (5 nodes) to 1 (2 nodes): below the majority.
	tr.setAlive(2, false)
	tr.setAlive(3, false)
	tr.setAlive(4, false)
	if err := lm.OnPeerFailure(2); err != nil {
		t.Fatalf("OnPeerFailure: %v", err)
	}

	if got := lm.ActiveLeases(); got != 0 {
		t.Fatalf("quorum loss must suspend all coordination, %d lease(s) remain", got)
	}
}

func TestSingleNodeClusterAlwaysHasQuorum(t *testing.T) {
	lm := NewLeaseManager(0, newFailoverTransport(0))
	lm.SetClusterSize(1)
	if !lm.HasQuorum() {
		t.Fatal("a single-node cluster must not block coordination")
	}
}

func TestCoordinationCounters(t *testing.T) {
	tr := newFailoverTransport(1, 2)
	lm := NewLeaseManager(1, tr)
	if h, q := lm.CoordinationCounters(); h != 0 || q != 0 {
		t.Fatalf("fresh counters must be zero, got %d/%d", h, q)
	}

	lm.held[1] = &Lease{LeaseID: 1, HolderID: 1, Key: "k", QuorumSize: 0}
	lm.ResignAll("test handoff")
	if h, _ := lm.CoordinationCounters(); h != 1 {
		t.Fatalf("handoff counter = %d, want 1", h)
	}
	// Resigning with nothing held must not count a handoff.
	_ = lm.ResignAll("nothing to do")
	if h, _ := lm.CoordinationCounters(); h != 1 {
		t.Fatalf("empty resignation must not increment, got %d", h)
	}

	lm.SetClusterSize(3)
	tr.setAlive(2, false)
	if err := lm.OnPeerFailure(2); err != nil {
		t.Fatalf("OnPeerFailure: %v", err)
	}
	if _, q := lm.CoordinationCounters(); q != 1 {
		t.Fatalf("quorum-loss counter = %d, want 1", q)
	}
}
