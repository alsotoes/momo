package transport

import (
	"bufio"
	"bytes"
	"io"
	"testing"
)

// BenchmarkAWSChunkedReaderSigned measures de-framing throughput for a large
// signed streaming payload using the AWS documentation chunk signatures.
func BenchmarkAWSChunkedReaderSigned(b *testing.B) {
	content := bytes.Repeat([]byte{'a'}, 66560)
	// Signatures under these credentials/date are valid for any 65536/1024 'a'
	// chunks chained from the fixed seed (used purely to exercise the verifier
	// path; the de-framer keys only off the seed request signature).
	wire := docWireBody()

	b.ReportAllocs()
	b.SetBytes(int64(len(content)))
	for i := 0; i < b.N; i++ {
		r := newAWSChunkedReader(bufio.NewReader(bytes.NewReader(wire)), docSigningCtx(), false, 1<<30)
		if _, err := io.Copy(io.Discard, r); err != nil {
			b.Fatalf("decode failed: %v", err)
		}
	}
}

// BenchmarkAWSChunkedReaderUnsigned measures de-framing throughput without
// per-chunk signature verification.
func BenchmarkAWSChunkedReaderUnsigned(b *testing.B) {
	content := bytes.Repeat([]byte{'a'}, 66560)
	wire := "10000\r\n" + string(content[:65536]) + "\r\n400\r\n" + string(content[65536:]) + "\r\n0\r\n\r\n"

	b.ReportAllocs()
	b.SetBytes(int64(len(content)))
	for i := 0; i < b.N; i++ {
		r := newAWSChunkedReader(bufio.NewReader(bytes.NewBufferString(wire)), nil, false, 1<<30)
		if _, err := io.Copy(io.Discard, r); err != nil {
			b.Fatalf("decode failed: %v", err)
		}
	}
}
