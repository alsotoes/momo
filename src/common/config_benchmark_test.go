package common

import (
	"strconv"
	"strings"
	"testing"
)

func BenchmarkParseReplicationOrder_NoPrealloc(b *testing.B) {
	replicationOrderStr := "1, 2, 3, 4, 5, 6, 7, 8, 9, 10"
	parts := strings.Split(replicationOrderStr, ",")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var orderSlice []int
		for _, part := range parts {
			order, _ := strconv.Atoi(strings.TrimSpace(part))
			orderSlice = append(orderSlice, order)
		}
		_ = orderSlice
	}
}

func BenchmarkParseReplicationOrder_Prealloc(b *testing.B) {
	replicationOrderStr := "1, 2, 3, 4, 5, 6, 7, 8, 9, 10"
	parts := strings.Split(replicationOrderStr, ",")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		orderSlice := make([]int, 0, len(parts))
		for _, part := range parts {
			order, _ := strconv.Atoi(strings.TrimSpace(part))
			orderSlice = append(orderSlice, order)
		}
		_ = orderSlice
	}
}
