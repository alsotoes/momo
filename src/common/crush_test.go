package common

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strings"
	"syscall"
	"testing"

	"go.uber.org/goleak"
)

func TestClusterMap_Placement(t *testing.T) {
	defer goleak.VerifyNone(t)

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
	defer goleak.VerifyNone(t)

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
	defer goleak.VerifyNone(t)

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

	// Test no eligible (positive-weight) nodes: nil/zero-weight nodes are filtered
	mPanic := &ClusterMap{Nodes: []*Node{nil, {ID: 0, Weight: 0}}}
	_, err = mPanic.Placement("some-hash", 1)
	if err == nil {
		t.Errorf("Expected error when no positive-weight nodes, got nil")
	}
	if !errors.Is(err, syscall.EINVAL) {
		t.Errorf("Expected error to wrap syscall.EINVAL, got %v", err)
	}
}

func TestClusterMap_LargeNodeIDs(t *testing.T) {
	defer goleak.VerifyNone(t)

	nodes := []*Node{
		{ID: 1, Weight: 1, Addr: "node-a"},
		{ID: 1 + (1 << 32), Weight: 1, Addr: "node-b"},
	}
	m := &ClusterMap{Nodes: nodes}

	p1, err := m.Placement("test-hash", 2)
	if err != nil {
		t.Fatalf("Placement failed: %v", err)
	}
	if len(p1) != 2 {
		t.Fatalf("Expected 2 nodes, got %d", len(p1))
	}
	if p1[0].ID == p1[1].ID {
		t.Fatalf("Node IDs collided: both are %d", p1[0].ID)
	}
}

func TestClusterMap_Placement_SkipsZeroWeightNodes(t *testing.T) {
	defer goleak.VerifyNone(t)

	nodes := []*Node{
		{ID: 0, Weight: 1, Addr: "127.0.0.1:4440"},
		{ID: 1, Weight: 1, Addr: "127.0.0.1:4441"},
		{ID: 2, Weight: 0, Addr: "127.0.0.1:4442"},  // disabled
		{ID: 3, Weight: -5, Addr: "127.0.0.1:4443"}, // decommissioned
	}
	m := &ClusterMap{Nodes: nodes}

	// With replicationFactor == 4 (>= len(nodes)), zero/negative-weight nodes
	// must never be selected even though scoring would sort them last.
	p, err := m.Placement("some-hash", 4)
	if err != nil {
		t.Fatalf("Placement failed: %v", err)
	}
	if len(p) != 2 {
		t.Fatalf("Expected 2 eligible nodes, got %d", len(p))
	}
	for _, node := range p {
		if node.Weight <= 0 {
			t.Errorf("Placement selected node %d with weight %d", node.ID, node.Weight)
		}
	}
}

func TestClusterMap_Placement_AllZeroWeight(t *testing.T) {
	defer goleak.VerifyNone(t)

	m := &ClusterMap{Nodes: []*Node{
		{ID: 0, Weight: 0, Addr: "127.0.0.1:4440"},
		{ID: 1, Weight: -1, Addr: "127.0.0.1:4441"},
	}}

	_, err := m.Placement("some-hash", 2)
	if err == nil {
		t.Fatal("Expected error when all nodes have Weight <= 0, got nil")
	}
	if !errors.Is(err, syscall.EINVAL) {
		t.Errorf("Expected error to wrap syscall.EINVAL, got %v", err)
	}
}

// TestClusterMap_Placement_CapWarning verifies that requesting a replication
// factor larger than the eligible node count logs a warning instead of silently
// capping (fix #645), and that no warning is logged when RF fits.
func TestClusterMap_Placement_CapWarning(t *testing.T) {
	defer goleak.VerifyNone(t)

	nodes := []*Node{
		{ID: 0, Weight: 1, Addr: "127.0.0.1:4440"},
		{ID: 1, Weight: 1, Addr: "127.0.0.1:4441"},
	}
	m := &ClusterMap{Nodes: nodes}

	wantWarning := func(label, wantSub, forbiddenSub string, rf int, wantLen int) {
		t.Helper()
		var buf bytes.Buffer
		prev := log.Writer()
		log.SetOutput(&buf)
		defer log.SetOutput(prev)

		out, err := m.Placement("some-hash", rf)
		if err != nil {
			t.Fatalf("%s: Placement failed: %v", label, err)
		}
		if len(out) != wantLen {
			t.Fatalf("%s: Expected %d nodes, got %d", label, wantLen, len(out))
		}
		if wantSub != "" && !strings.Contains(buf.String(), wantSub) {
			t.Errorf("%s: Expected log to contain %q, got %q", label, wantSub, buf.String())
		}
		if forbiddenSub != "" && strings.Contains(buf.String(), forbiddenSub) {
			t.Errorf("%s: Expected log to NOT contain %q, got %q", label, forbiddenSub, buf.String())
		}
	}

	// RF=5 with 2 eligible nodes: capped to 2, warning logged.
	wantWarning("oversized RF", "replication factor 5 exceeds 2 eligible nodes", "", 5, 2)
	// RF=2 with 2 nodes: no capping, no warning.
	wantWarning("fitting RF", "", "exceeds", 2, 2)
}
