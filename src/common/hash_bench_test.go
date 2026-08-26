package common

import (
	"os"
	"path/filepath"
	"testing"
)

var hashSizes = []struct {
	name string
	size int
}{
	{"1MiB", 1 << 20},
	{"64MiB", 64 << 20},
	{"256MiB", 256 << 20},
}

// BenchmarkHashBytes measures in-memory SHA-256 content hashing cost
// (Win-1 candidate surface). Setup allocates the input once and reports
// bytes so ns/op scales with payload size.
func BenchmarkHashBytes(b *testing.B) {
	for _, sz := range hashSizes {
		b.Run(sz.name, func(b *testing.B) {
			data := make([]byte, sz.size)
			b.SetBytes(int64(sz.size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				HashBytes(data)
			}
		})
	}
}

// BenchmarkHashFile measures SHA-256 hashing of an on-disk blob
// (HashFile streams the file without allocating the whole payload).
func BenchmarkHashFile(b *testing.B) {
	for _, sz := range hashSizes {
		b.Run(sz.name, func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "blob.bin")
			payload := make([]byte, sz.size)
			if err := os.WriteFile(path, payload, 0600); err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(sz.size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := HashFile(path); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
