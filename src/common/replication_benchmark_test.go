package common

import (
	"testing"
)

func BenchmarkPadString(b *testing.B) {
	input := "some_test_string"
	length := 64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = PadString(input, length)
	}
}
