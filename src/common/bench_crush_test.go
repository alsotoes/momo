package common

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"testing"
)

func BenchmarkCrushOriginal(b *testing.B) {
	node := &Node{ID: 1, Weight: 100}
	objectHash := "some-object-hash"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := sha256.New()
		h.Write([]byte(objectHash))
		binary.Write(h, binary.LittleEndian, uint32(node.ID))
		sum := h.Sum(nil)

		val := binary.LittleEndian.Uint64(sum[:8])
		floatVal := float64(val) / float64(math.MaxUint64)

		var finalScore float64
		if floatVal > 0 && node.Weight > 0 {
			finalScore = -float64(node.Weight) / math.Log(floatVal)
		} else {
			finalScore = 0
		}
		_ = finalScore
	}
}

// BenchmarkPlacement measures the legacy (no failure domains) placement path
// (R1, #929): it must stay unchanged versus pre-R1 numbers.
func BenchmarkPlacement(b *testing.B) {
	nodes := make([]*Node, 10)
	for i := range nodes {
		nodes[i] = &Node{ID: i, Weight: i + 1, Addr: "node"}
	}
	m := &ClusterMap{Nodes: nodes}
	objectHash := "some-object-hash"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Placement(objectHash, 3); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPlacementDomainSpread measures the failure-domain spread path (R1).
func BenchmarkPlacementDomainSpread(b *testing.B) {
	domains := []string{"rack-a", "rack-b", "rack-c"}
	nodes := make([]*Node, 10)
	for i := range nodes {
		nodes[i] = &Node{ID: i, Weight: i + 1, Addr: "node", Domain: domains[i%len(domains)]}
	}
	m := &ClusterMap{Nodes: nodes}
	objectHash := "some-object-hash"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Placement(objectHash, 3); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCrushOptimized(b *testing.B) {
	node := &Node{ID: 1, Weight: 100}
	objectHash := "some-object-hash"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := sha256.New()
		h.Write([]byte(objectHash))

		var idBuf [4]byte
		binary.LittleEndian.PutUint32(idBuf[:], uint32(node.ID))
		h.Write(idBuf[:])

		var sumBuf [sha256.Size]byte
		sum := h.Sum(sumBuf[:0])

		val := binary.LittleEndian.Uint64(sum[:8])
		floatVal := float64(val) / float64(math.MaxUint64)

		var finalScore float64
		if floatVal > 0 && node.Weight > 0 {
			finalScore = -float64(node.Weight) / math.Log(floatVal)
		} else {
			finalScore = 0
		}
		_ = finalScore
	}
}
