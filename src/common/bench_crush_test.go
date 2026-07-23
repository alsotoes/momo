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
