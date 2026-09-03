package transport

import (
	"net/http"
	"testing"
	"time"
)

// BenchmarkLastModifiedHeader_AppendFormat measures the new allocation-free
// rendering used in HandshakeServer for GET/HEAD/304 Last-Modified headers.
func BenchmarkLastModifiedHeader_AppendFormat(b *testing.B) {
	var buf [16384]byte
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bb := buf[:0]
		bb = append(bb, "HTTP/1.1 200 OK\r\nLast-Modified: "...)
		bb = time.Unix(0, int64(i)*1e9).UTC().AppendFormat(bb, http.TimeFormat)
		bb = append(bb, "\r\n"...)
	}
}

// BenchmarkLastModifiedHeader_Format measures the previous time.Format
// implementation for comparison.
func BenchmarkLastModifiedHeader_Format(b *testing.B) {
	var buf [16384]byte
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bb := buf[:0]
		bb = append(bb, "HTTP/1.1 200 OK\r\nLast-Modified: "...)
		bb = append(bb, time.Unix(0, int64(i)*1e9).UTC().Format(http.TimeFormat)...)
		bb = append(bb, "\r\n"...)
	}
}
