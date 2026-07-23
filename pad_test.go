package main

import "testing"

func PadStringMaster(input string, length int) string {
	if len(input) >= length {
		return input[:length]
	}
	b := make([]byte, length)
	copy(b, input)
	return string(b)
}

func PadStringNew(input string, length int) string {
	if len(input) >= length {
		return input[:length]
	}
	if length <= 256 {
		var buf [256]byte
		copy(buf[:], input)
		return string(buf[:length])
	}
	b := make([]byte, length)
	copy(b, input)
	return string(b)
}

func BenchmarkMaster(b *testing.B) {
	for i := 0; i < b.N; i++ {
		PadStringMaster("hello", 64)
	}
}

func BenchmarkNew(b *testing.B) {
	for i := 0; i < b.N; i++ {
		PadStringNew("hello", 64)
	}
}
