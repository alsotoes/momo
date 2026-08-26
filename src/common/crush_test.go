package common

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
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

// TestHashToScoreValue_Precision verifies the 52-bit mantissa fold does not
// lose precision for hashes that differ only in their low bits (fix #647).
// A full uint64 → float64 conversion would round those differences away.
func TestHashToScoreValue_Precision(t *testing.T) {
	base := make([]byte, 32)
	// Single lowest-bit difference in the folded tail (sum[28] is the LSB of
	// the little-endian tail uint32, inside the 20-bit mask).
	low := make([]byte, 32)
	low[28] = 0x01

	// Difference in the top bucket bits: sum[3] is the MSB of the little-endian
	// head uint32; 0x80 there sets mantissa 1<<51 (exact 0.5).
	high := make([]byte, 32)
	high[3] = 0x80

	vBase := hashToScoreValue(base)
	vLow := hashToScoreValue(low)
	vHigh := hashToScoreValue(high)

	if vBase == vLow {
		t.Errorf("Expected distinct scores for digests differing in low tail bit, both %v", vBase)
	}
	if vHigh <= vBase {
		t.Errorf("Expected digest with high bit to score above all-zero digest, got %v <= %v", vHigh, vBase)
	}

	// Determinism and range.
	if vBase != hashToScoreValue(make([]byte, 32)) {
		t.Error("hashToScoreValue is not deterministic")
	}
	if vHigh < 0 || vHigh >= 1.0 {
		t.Errorf("Score out of [0,1) range: %v", vHigh)
	}

	// Mantissa 1<<51 is exactly representable: score must be exactly 0.5.
	if vHigh != 0.5 {
		t.Errorf("Expected exactly 0.5 for 1<<51 mantissa, got %v", vHigh)
	}
}

// TestClusterMap_Placement_TiedScoresStableOrder verifies that nodes with equal
// scores keep their declaration order in the result (fix #646). Two nodes with
// identical ID and weight hash to identical float values, guaranteeing a score
// tie; the stable sort must keep the first-declared tied node before the other.
func TestClusterMap_Placement_TiedScoresStableOrder(t *testing.T) {
	defer goleak.VerifyNone(t)

	m := &ClusterMap{Nodes: []*Node{
		{ID: 1, Weight: 1, Addr: "tie-a"},
		{ID: 1, Weight: 1, Addr: "tie-b"},
		{ID: 2, Weight: 10, Addr: "heavy"},
	}}

	idx := func(addrs []*Node, want string) int {
		for i, n := range addrs {
			if n.Addr == want {
				return i
			}
		}
		return -1
	}

	for run := 0; run < 50; run++ {
		// RF=3 selects every node: the full ordering must preserve the tied
		// pair's declaration order (tie-a before tie-b) on every run.
		out, err := m.Placement("some-hash", 3)
		if err != nil {
			t.Fatalf("run %d: Placement failed: %v", run, err)
		}
		ia, ib := idx(out, "tie-a"), idx(out, "tie-b")
		if ia == -1 || ib == -1 {
			t.Fatalf("run %d: tied nodes missing from placement: %+v", run, out)
		}
		if ia > ib {
			t.Fatalf("run %d: stable sort violated: tie-a at %d, tie-b at %d", run, ia, ib)
		}
	}
}

// testFinalScore recomputes the WRH score of a node for an object hash,
// mirroring Placement's scoring, so tests can verify the selected set against
// a brute-force reference without exporting internals.
func testFinalScore(objectHash string, node *Node) float64 {
	h := sha256.New()
	h.Write([]byte(objectHash))
	var idBuf [8]byte
	binary.LittleEndian.PutUint64(idBuf[:], uint64(node.ID))
	h.Write(idBuf[:])
	v := hashToScoreValue(h.Sum(nil))
	if v > 0 && v < 1.0 && node.Weight > 0 {
		return -float64(node.Weight) / math.Log(v)
	}
	return 0
}

// bruteForceBestPlacement enumerates all replica sets of size r and returns the
// R1-C2 reference optimum: maximal distinct-domain count, then maximal
// finalScore sum.
func bruteForceBestPlacement(nodes []*Node, r int, objectHash string) (bestDomains int, bestSum float64) {
	bestDomains = -1
	var rec func(start int, combo []*Node)
	rec = func(start int, combo []*Node) {
		if len(combo) == r {
			domains := make(map[string]struct{}, r)
			sum := 0.0
			for _, n := range combo {
				domains[n.Domain] = struct{}{}
				sum += testFinalScore(objectHash, n)
			}
			if len(domains) > bestDomains || (len(domains) == bestDomains && sum > bestSum) {
				bestDomains, bestSum = len(domains), sum
			}
			return
		}
		for i := start; i < len(nodes); i++ {
			rec(i+1, append(combo, nodes[i]))
		}
	}
	rec(0, nil)
	return bestDomains, bestSum
}

// TestClusterMap_Placement_FailureDomainSpread (R1-T1) verifies placement
// maximizes distinct failure domains and matches the brute-force optimum
// across same-domain, multi-domain, and partial-domain topologies (#929).
func TestClusterMap_Placement_FailureDomainSpread(t *testing.T) {
	defer goleak.VerifyNone(t)

	topologies := []struct {
		name   string
		domain []string // per-node domain; "" = unclassified
		rf     int
	}{
		{"all same domain", []string{"rack-a", "rack-a", "rack-a"}, 2},
		{"all distinct domains", []string{"rack-a", "rack-b", "rack-c"}, 3},
		{"spread over duplicate domain", []string{"rack-a", "rack-a", "rack-b", "rack-c"}, 3},
		{"partial domains with unclassified", []string{"rack-a", "", "rack-b", ""}, 3},
		{"degraded: fewer domains than replicas", []string{"rack-a", "rack-a", "rack-b", "rack-b"}, 3},
		{"full unclassified is one default domain", []string{"", "", ""}, 2},
	}

	const objectHash = "eb0e30ff02be45f64a19881497f0f4233a9cfb674243e652d6299bf176551897"

	for _, tc := range topologies {
		t.Run(tc.name, func(t *testing.T) {
			nodes := make([]*Node, len(tc.domain))
			for i, dom := range tc.domain {
				nodes[i] = &Node{ID: i, Weight: 1, Addr: fmt.Sprintf("node-%d", i), Domain: dom}
			}
			m := &ClusterMap{Nodes: nodes}

			p, err := m.Placement(objectHash, tc.rf)
			if err != nil {
				t.Fatalf("Placement failed: %v", err)
			}
			if len(p) != tc.rf {
				t.Fatalf("expected %d replicas, got %d", tc.rf, len(p))
			}

			wantDomains, wantSum := bruteForceBestPlacement(nodes, tc.rf, objectHash)
			gotDomains := make(map[string]struct{}, tc.rf)
			gotSum := 0.0
			seen := make(map[int]struct{}, tc.rf)
			for _, n := range p {
				if _, dup := seen[n.ID]; dup {
					t.Fatalf("node %d selected twice", n.ID)
				}
				seen[n.ID] = struct{}{}
				gotDomains[n.Domain] = struct{}{}
				gotSum += testFinalScore(objectHash, n)
			}
			if len(gotDomains) != wantDomains {
				t.Errorf("distinct domains = %d, want optimum %d", len(gotDomains), wantDomains)
			}
			if math.Abs(gotSum-wantSum) > 1e-9*math.Max(1, math.Abs(wantSum)) {
				t.Errorf("finalScore sum = %v, want optimum %v", gotSum, wantSum)
			}
		})
	}
}

// TestClusterMap_Placement_FailureDomainDeterminism (R1-T2) verifies identical
// topology + hash yields identical placement across calls with domains set.
func TestClusterMap_Placement_FailureDomainDeterminism(t *testing.T) {
	defer goleak.VerifyNone(t)

	m := &ClusterMap{Nodes: []*Node{
		{ID: 0, Weight: 1, Addr: "n0", Domain: "rack-a"},
		{ID: 1, Weight: 2, Addr: "n1", Domain: "rack-a"},
		{ID: 2, Weight: 1, Addr: "n2", Domain: "rack-b"},
		{ID: 3, Weight: 3, Addr: "n3", Domain: ""},
	}}

	want, err := m.Placement("some-hash", 3)
	if err != nil {
		t.Fatalf("Placement failed: %v", err)
	}
	for run := 0; run < 50; run++ {
		got, err := m.Placement("some-hash", 3)
		if err != nil {
			t.Fatalf("run %d: Placement failed: %v", run, err)
		}
		for i := range want {
			if got[i].ID != want[i].ID {
				t.Fatalf("run %d: non-deterministic at index %d: got %d, want %d", run, i, got[i].ID, want[i].ID)
			}
		}
	}
}

// TestClusterMap_Placement_FailureDomainDegradedWarning (R1-T3) verifies a
// warning is logged when R exceeds the distinct-domain count but R replicas
// are still returned, and that no warning fires when the constraint holds.
func TestClusterMap_Placement_FailureDomainDegradedWarning(t *testing.T) {
	defer goleak.VerifyNone(t)

	m := &ClusterMap{Nodes: []*Node{
		{ID: 0, Weight: 1, Addr: "n0", Domain: "rack-a"},
		{ID: 1, Weight: 1, Addr: "n1", Domain: "rack-a"},
		{ID: 2, Weight: 1, Addr: "n2", Domain: "rack-b"},
	}}

	check := func(label, wantSub, forbiddenSub string, rf int) {
		t.Helper()
		var buf bytes.Buffer
		prev := log.Writer()
		log.SetOutput(&buf)
		defer log.SetOutput(prev)

		out, err := m.Placement("some-hash", rf)
		if err != nil {
			t.Fatalf("%s: Placement failed: %v", label, err)
		}
		if len(out) != rf {
			t.Fatalf("%s: expected %d replicas, got %d", label, rf, len(out))
		}
		if wantSub != "" && !strings.Contains(buf.String(), wantSub) {
			t.Errorf("%s: expected log to contain %q, got %q", label, wantSub, buf.String())
		}
		if forbiddenSub != "" && strings.Contains(buf.String(), forbiddenSub) {
			t.Errorf("%s: expected log to NOT contain %q, got %q", label, forbiddenSub, buf.String())
		}
	}

	// 2 distinct domains, R=3: degraded, warning, still 3 replicas.
	check("degraded", "exceeds 2 distinct failure domains", "", 3)
	// 2 distinct domains, R=2: constraint satisfied, no warning.
	check("satisfiable", "", "failure domains", 2)
}

// TestClusterMap_Placement_NoDomainsLegacyPath verifies that when no node
// declares a Domain the legacy top-R selection runs unchanged and no
// failure-domain warning is logged (backward compatibility, R1-T4).
func TestClusterMap_Placement_NoDomainsLegacyPath(t *testing.T) {
	defer goleak.VerifyNone(t)

	nodes := []*Node{
		{ID: 0, Weight: 1, Addr: "n0"},
		{ID: 1, Weight: 2, Addr: "n1"},
		{ID: 2, Weight: 1, Addr: "n2"},
		{ID: 3, Weight: 3, Addr: "n3"},
	}
	m := &ClusterMap{Nodes: nodes}

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	const hash = "some-hash"
	p, err := m.Placement(hash, 2)
	if err != nil {
		t.Fatalf("Placement failed: %v", err)
	}
	if strings.Contains(buf.String(), "failure domains") {
		t.Errorf("legacy path logged failure-domain warning: %q", buf.String())
	}

	// Result must equal the legacy top-R ordering (score desc, stable ties).
	want := append([]*Node(nil), nodes...)
	sort.SliceStable(want, func(i, j int) bool {
		return testFinalScore(hash, want[i]) > testFinalScore(hash, want[j])
	})
	for i := range p {
		if p[i].ID != want[i].ID {
			t.Errorf("index %d: got node %d, want legacy top-R node %d", i, p[i].ID, want[i].ID)
		}
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
