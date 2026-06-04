package main

import (
	"strings"
	"testing"
)

func BenchmarkSplit(b *testing.B) {
	s := "1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parts := strings.Split(s, ",")
		_ = parts
	}
}

func BenchmarkNoSplit(b *testing.B) {
	s := "1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		str := s
		for {
			idx := strings.IndexByte(str, ',')
			if idx == -1 {
				_ = str
				break
			}
			_ = str[:idx]
			str = str[idx+1:]
		}
	}
}
