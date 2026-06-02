package common

import (
	"os"
	"testing"
)

func BenchmarkHashFile(b *testing.B) {
	tmpfile, err := os.CreateTemp("", "bench.txt")
	if err != nil {
		b.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Write([]byte("hello world, let's benchmark this hashing function with some data."))
	tmpfile.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HashFile(tmpfile.Name())
	}
}
