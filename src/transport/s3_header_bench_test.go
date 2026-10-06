package transport

import (
	"net/http"
	"strings"
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

// BenchmarkEtagMatches measures the zero-allocation etagMatches scan against a
// realistic multi-tag If-Match header (the target is the last entry).
func BenchmarkEtagMatches(b *testing.B) {
	header := `"aaa111", "bbb222", "ccc333", W/"ddd444", "target"` // notsecret
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !etagMatches(header, "target") {
			b.Fatal("expected match")
		}
	}
}

// BenchmarkEtagMatchesSplit is the previous strings.Split-based implementation,
// kept only as a comparison baseline.
func BenchmarkEtagMatchesSplit(b *testing.B) {
	header := `"aaa111", "bbb222", "ccc333", W/"ddd444", "target"` // notsecret
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !etagMatchesSplit(header, "target") {
			b.Fatal("expected match")
		}
	}
}

func etagMatchesSplit(header, etag string) bool {
	for _, item := range strings.Split(header, ",") {
		item = strings.TrimSpace(item)
		if item == "*" {
			return true
		}
		item = strings.TrimPrefix(item, "W/")
		item = strings.TrimSpace(item)
		item = strings.Trim(item, `"`)
		if item == etag {
			return true
		}
	}
	return false
}
