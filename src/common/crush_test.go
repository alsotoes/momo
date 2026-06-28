package common

import (
	"errors"
	"fmt"
	"syscall"
	"testing"
)

func TestClusterMap_Placement(t *testing.T) {
	nodes := []*Node{
		{ID: 0, Weight: 1, Addr: "127.0.0.1:4440"},
		{ID: 1, Weight: 1, Addr: "127.0.0.1:4441"},
		{ID: 2, Weight: 1, Addr: "127.0.0.1:4442"},
	}
	m := &ClusterMap{Nodes: nodes}

	objectHash := "eb0e30ff02be45f64a19881497f0f4233a9cfb674243e652d6299bf176551897"

	// 1. Deterministic Placement
	p1, _ := m.Placement(objectHash, 2)
	p2, _ := m.Placement(objectHash, 2)

	if len(p1) != 2 || len(p2) != 2 {
		t.Fatalf("Expected 2 nodes, got %d and %d", len(p1), len(p2))
	}

	for i := range p1 {
		if p1[i].ID != p2[i].ID {
			t.Errorf("Placement is not deterministic at index %d", i)
		}
	}

	// 2. Load Distribution (informational)
	distribution := make(map[int]int)
	for i := 0; i < 1000; i++ {
		hash := fmt.Sprintf("hash-%d", i)
		nodes, _ := m.Placement(hash, 1)
		distribution[nodes[0].ID]++
	}

	t.Logf("Load distribution over 1000 objects: %v", distribution)
	
	// Ensure all nodes got some load
	for _, node := range nodes {
		if distribution[node.ID] == 0 {
			t.Errorf("Node %d got zero load", node.ID)
		}
	}
}

func TestClusterMap_Weighting(t *testing.T) {
	nodes := []*Node{
		{ID: 0, Weight: 10, Addr: "big-node"},
		{ID: 1, Weight: 1, Addr: "small-node"},
	}
	m := &ClusterMap{Nodes: nodes}

	distribution := make(map[int]int)
	for i := 0; i < 1000; i++ {
		hash := fmt.Sprintf("hash-%d", i)
		nodes, _ := m.Placement(hash, 1)
		distribution[nodes[0].ID]++
	}

	t.Logf("Weighted distribution: %v", distribution)
	if distribution[0] <= distribution[1] {
		t.Errorf("Expected node 0 (weight 10) to have more load than node 1 (weight 1), got %v", distribution)
	}
}

func TestClusterMap_Placement_Defensive(t *testing.T) {
	nodes := []*Node{
		{ID: 0, Weight: 1, Addr: "127.0.0.1:4440"},
	}
	m := &ClusterMap{Nodes: nodes}

	// Test empty object hash
	_, err := m.Placement("", 1)
	if err == nil {
		t.Errorf("Expected error for empty object hash, got nil")
	}
	if !errors.Is(err, syscall.EINVAL) {
		t.Errorf("Expected error to wrap syscall.EINVAL, got %v", err)
	}

	// Test zero replication factor
	_, err = m.Placement("some-hash", 0)
	if err == nil {
		t.Errorf("Expected error for zero replication factor, got nil")
	}
	if !errors.Is(err, syscall.EINVAL) {
		t.Errorf("Expected error to wrap syscall.EINVAL, got %v", err)
	}

	// Test negative replication factor
	_, err = m.Placement("some-hash", -5)
	if err == nil {
		t.Errorf("Expected error for negative replication factor, got nil")
	}
	if !errors.Is(err, syscall.EINVAL) {
		t.Errorf("Expected error to wrap syscall.EINVAL, got %v", err)
	}

	// Test empty cluster map nodes
	mEmpty := &ClusterMap{Nodes: []*Node{}}
	_, err = mEmpty.Placement("some-hash", 1)
	if err == nil {
		t.Errorf("Expected error for empty cluster map, got nil")
	}
	if !errors.Is(err, syscall.EINVAL) {
		t.Errorf("Expected error to wrap syscall.EINVAL, got %v", err)
	}
}
