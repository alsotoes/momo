package common

import (
	"os"
	"testing"
)

func BenchmarkHashFile(b *testing.B) {
	tmpfile, err := os.CreateTemp("", "bench_test.txt")
	if err != nil {
		b.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Write([]byte("hello world"))
	tmpfile.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := HashFile(tmpfile.Name())
		if err != nil {
			b.Fatal(err)
		}
	}
}
