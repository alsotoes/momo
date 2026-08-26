package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"testing"
)

var perfSizes = []struct {
	name string
	size int
}{
	{"1MiB", 1 << 20},
	{"64MiB", 64 << 20},
	{"256MiB", 256 << 20},
}

func newBenchPayload(size int) []byte {
	data := make([]byte, size)
	for i := 0; i < size && i < len(data); i++ {
		data[i] = byte(i * 31)
	}
	return data
}

func benchPayloadHash(data []byte) string {
	sum := sha256.Sum256(data)
	var hexBuf [sha256.Size * 2]byte
	hex.Encode(hexBuf[:], sum[:])
	return string(hexBuf[:])
}

// BenchmarkLocalWrite measures the 64KB-buffered local blobstore write path
// (os.TempDir-backed). A unique hash per iteration forces a fresh path write.
func BenchmarkLocalWrite(b *testing.B) {
	for _, sz := range perfSizes {
		b.Run(sz.name, func(b *testing.B) {
			store, err := NewLocalBlobStore(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			defer store.Close()
			payload := newBenchPayload(sz.size)
			b.SetBytes(int64(sz.size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				hash := fmt.Sprintf("%064x", i)
				if err := store.PutBlob(hash, bytes.NewReader(payload)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkReadVerify measures the cost of verify-on-read over a full
// in-memory blob: streaming the bytes through the storing hasher and
// asserting the content-address (the current per-read SHA-256 cost).
func BenchmarkReadVerify(b *testing.B) {
	for _, sz := range perfSizes {
		b.Run(sz.name, func(b *testing.B) {
			payload := newBenchPayload(sz.size)
			expected := benchPayloadHash(payload)
			b.SetBytes(int64(sz.size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				v := newVerifyingReader(bytes.NewReader(payload), expected)
				if _, err := io.Copy(io.Discard, v); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkS3PutSpool measures the S3 spool+hash single pass
// (os.CreateTemp + io.MultiWriter(spill, hasher) over a size-bounded
// reader), excluding the HTTP round trip.
func BenchmarkS3PutSpool(b *testing.B) {
	for _, sz := range perfSizes {
		b.Run(sz.name, func(b *testing.B) {
			payload := newBenchPayload(sz.size)
			b.SetBytes(int64(sz.size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				spill, err := os.CreateTemp("", "momo-s3-put-*")
				if err != nil {
					b.Fatal(err)
				}
				hasher := sha256.New()
				if _, err := io.Copy(io.MultiWriter(spill, hasher), bytes.NewReader(payload)); err != nil {
					_ = spill.Close()
					b.Fatal(err)
				}
				_ = spill.Close()
				_ = os.Remove(spill.Name())
			}
		})
	}
}
