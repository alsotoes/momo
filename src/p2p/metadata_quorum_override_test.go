package p2p

import "testing"

// TestWriteQuorumOverride pins the wiring behind #1323: [momofs]
// metadata_quorum decides how many replicas must acknowledge, instead of the
// hardcoded strict majority.
func TestWriteQuorumOverride(t *testing.T) {
	p := NewMetadataRPCProvider(0, nil, NewRing([]int32{0, 1, 2, 3, 4}, 3), nil)

	if got := p.writeQuorum(5); got != 3 {
		t.Fatalf("default must be a strict majority (3 of 5), got %d", got)
	}

	p.SetWriteQuorum(2)
	if got := p.writeQuorum(5); got != 2 {
		t.Fatalf("configured quorum 2 must be honoured, got %d", got)
	}

	// A quorum larger than the replica set would make every write fail; clamping
	// keeps a misconfiguration from taking the cluster down.
	p.SetWriteQuorum(9)
	if got := p.writeQuorum(3); got != 3 {
		t.Fatalf("quorum must clamp to the replica count, got %d", got)
	}

	// Zero restores the default; negatives are ignored rather than trusted.
	p.SetWriteQuorum(0)
	if got := p.writeQuorum(4); got != 3 {
		t.Fatalf("zero must restore majority (3 of 4), got %d", got)
	}
	p.SetWriteQuorum(-5)
	if got := p.writeQuorum(4); got != 3 {
		t.Fatalf("a negative override must fall back to majority, got %d", got)
	}

	// A single replica always needs exactly one ack.
	p.SetWriteQuorum(5)
	if got := p.writeQuorum(1); got != 1 {
		t.Fatalf("a single replica needs one ack, got %d", got)
	}
}

// TestMajorityQuorumUnchanged guards the shared helper the default path uses.
func TestMajorityQuorumUnchanged(t *testing.T) {
	for _, tc := range []struct{ cluster, want int }{
		{0, 0}, {1, 1}, {2, 2}, {3, 2}, {4, 3}, {5, 3},
	} {
		if got := majorityQuorum(tc.cluster); got != tc.want {
			t.Fatalf("majorityQuorum(%d) = %d, want %d", tc.cluster, got, tc.want)
		}
	}
}
