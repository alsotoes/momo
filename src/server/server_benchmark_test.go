package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
)

// BenchmarkConcurrentUploads benchmarks how fast the Daemon handles multiple concurrent file uploads.
func BenchmarkConcurrentUploads(b *testing.B) {
	tempDir := b.TempDir()
	daemons := []*momo_common.Daemon{
		{Host: "127.0.0.1:45690", Data: tempDir},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the server
	go Daemon(ctx, daemons, 0)
	time.Sleep(100 * time.Millisecond)

	// Create a dummy file to upload
	fileContent := []byte("benchmarking data for momo concurrent uploads")
	file, err := os.CreateTemp("", "bench_file_*.txt")
	if err != nil {
		b.Fatalf("Failed to create bench file: %v", err)
	}
	file.Write(fileContent)
	file.Close()
	defer os.Remove(file.Name())

	md5, _ := momo_common.HashFile(file.Name())

	b.ResetTimer() // Reset timer so setup isn't counted

	var wg sync.WaitGroup

	// Run b.N concurrent client uploads
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			conn, err := net.Dial("tcp", "127.0.0.1:45690")
			if err != nil {
				b.Errorf("Dial failed: %v", err)
				return
			}
			defer conn.Close()

			timestampStr := padBenchString("123", momo_common.TimestampLength)
			conn.Write([]byte(timestampStr))

			buf := make([]byte, 1)
			conn.Read(buf) // Replication mode

			conn.Write([]byte(padBenchString(md5, 32)))
			conn.Write([]byte(padBenchString(fmt.Sprintf("bench_%d.txt", index), momo_common.FileInfoLength)))
			conn.Write([]byte(padBenchString(fmt.Sprintf("%d", len(fileContent)), momo_common.FileInfoLength)))

			conn.Write(fileContent)

			ackBuf := make([]byte, 4)
			conn.Read(ackBuf)
		}(i)
	}

	wg.Wait()
}

func padBenchString(input string, length int) string {
	if len(input) >= length {
		return input[:length]
	}
	return input + string(make([]byte, length-len(input)))
}
