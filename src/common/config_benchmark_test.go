package common

import (
	"strconv"
	"strings"
	"testing"
)

func BenchmarkParseReplicationOrder_NoPrealloc(b *testing.B) {
	str := "1,2,3,4,5,6,7,8,9,10"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parts := strings.Split(str, ",")
		var res []int
		for _, part := range parts {
			val, _ := strconv.Atoi(strings.TrimSpace(part))
			res = append(res, val)
		}
	}
}

func BenchmarkParseReplicationOrder_Prealloc(b *testing.B) {
	str := "1,2,3,4,5,6,7,8,9,10"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parts := strings.Split(str, ",")
		res := make([]int, 0, len(parts))
		for _, part := range parts {
			val, _ := strconv.Atoi(strings.TrimSpace(part))
			res = append(res, val)
		}
	}
}
