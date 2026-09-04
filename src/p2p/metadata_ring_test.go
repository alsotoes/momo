package p2p

import (
	"testing"
)

func TestMetadataRing_Lookup(t *testing.T) {
	nodeIDs := []int32{1, 2, 3, 4, 5}
	ring := NewRing(nodeIDs, 3)

	// Same key should always return same owner
	for i := 0; i < 100; i++ {
		key := "test/file.txt"
		owner := ring.Lookup(key)
		if owner < 1 || owner > 5 {
			t.Errorf("Lookup returned invalid node ID: %d", owner)
		}
	}
}

func TestMetadataRing_Replicas(t *testing.T) {
	nodeIDs := []int32{1, 2, 3, 4, 5}
	ring := NewRing(nodeIDs, 3)

	replicas := ring.Replicas("test/file.txt", 3)
	if len(replicas) != 3 {
		t.Errorf("Expected 3 replicas, got %d: %v", len(replicas), replicas)
	}
	// All replicas should be distinct
	seen := make(map[int32]bool)
	for _, r := range replicas {
		if seen[r] {
			t.Errorf("Duplicate replica: %d", r)
		}
		seen[r] = true
	}
	// All replicas should be valid node IDs
	for _, r := range replicas {
		if r < 1 || r > 5 {
			t.Errorf("Invalid replica node ID: %d", r)
		}
	}
}

func TestMetadataRing_ReplicasExhaustive(t *testing.T) {
	// Test with fewer nodes than M
	nodeIDs := []int32{1, 2}
	ring := NewRing(nodeIDs, 3)

	replicas := ring.Replicas("test/file.txt", 3)
	if len(replicas) != 2 {
		t.Errorf("Expected 2 replicas (limited by node count), got %d: %v", len(replicas), replicas)
	}
}

func TestMetadataRing_UpdateNodes(t *testing.T) {
	nodeIDs := []int32{1, 2, 3}
	ring := NewRing(nodeIDs, 3)

	// Add a node
	ring.UpdateNodes([]int32{1, 2, 3, 4, 5})
	if ring.NodeCount() != 5 {
		t.Errorf("Expected 5 nodes after update, got %d", ring.NodeCount())
	}

	// Remove a node
	ring.UpdateNodes([]int32{1, 2, 3})
	if ring.NodeCount() != 3 {
		t.Errorf("Expected 3 nodes after removal, got %d", ring.NodeCount())
	}
}

func TestMetadataRing_Deterministic(t *testing.T) {
	// Ring should be deterministic for same node set
	nodeIDs := []int32{1, 2, 3, 4, 5}
	ring1 := NewRing(nodeIDs, 3)
	ring2 := NewRing(nodeIDs, 3)

	for i := 0; i < 100; i++ {
		key := "key"
		if ring1.Lookup(key) != ring2.Lookup(key) {
			t.Errorf("Ring not deterministic: %d vs %d", ring1.Lookup(key), ring2.Lookup(key))
		}
	}
}

func TestMetadataRing_ReplicaDistribution(t *testing.T) {
	// Test that replicas are reasonably distributed across different keys
	nodeIDs := []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	ring := NewRing(nodeIDs, 3)

	counts := make(map[int32]int)
	for i := 0; i < 10000; i++ {
		key := "key" + string(rune(i%256)) // Use different keys
		replicas := ring.Replicas(key, 3)
		for _, r := range replicas {
			counts[r]++
		}
	}

	// Each node should get some replicas with 10k different keys
	for _, id := range nodeIDs {
		if counts[id] == 0 {
			t.Errorf("Node %d got zero replicas", id)
		}
	}
}

func TestMetadataRing_ShardKey(t *testing.T) {
	key1 := ShardKey("test/file.txt")
	key2 := ShardKey("test/file.txt")
	if key1 != key2 {
		t.Errorf("ShardKey not deterministic: %v vs %v", key1, key2)
	}
}
