package transport

import (
	"net/http"
	"testing"
)

// TestChecksumRequestHeaderTableMatchesResponseNaming guards the pre-canonicalized
// literals (⚡ Bolt, #1274): the canonical request header must be the canonical
// form of the lowercase response header the rest of the code uses.
func TestChecksumRequestHeaderTableMatchesResponseNaming(t *testing.T) {
	for _, h := range checksumRequestHeaders {
		want := http.CanonicalHeaderKey(checksumHeaderFor(h.algo))
		if h.header != want {
			t.Fatalf("algo %q: header %q, canonical form is %q", h.algo, h.header, want)
		}
	}
}

// TestParseChecksumFindsEveryAlgorithm proves the table-driven lookup is
// behaviourally identical to the previous dynamic construction.
func TestParseChecksumFindsEveryAlgorithm(t *testing.T) {
	for _, h := range checksumRequestHeaders {
		req, err := http.NewRequest(http.MethodPut, "http://momo/obj", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set(h.header, "aGVsbG8=")

		algo, expected, perr := parseChecksum(req)
		if perr != nil {
			t.Fatalf("algo %q: parseChecksum: %v", h.algo, perr)
		}
		if algo != h.algo || expected != "aGVsbG8=" {
			t.Fatalf("algo %q: got (%q,%q)", h.algo, algo, expected)
		}
	}
}

// TestCollectS3HeadersCapturesEveryChecksum proves persistence capture still sees
// each algorithm and records the inferred marker.
func TestCollectS3HeadersCapturesEveryChecksum(t *testing.T) {
	for _, h := range checksumRequestHeaders {
		req, err := http.NewRequest(http.MethodPut, "http://momo/obj", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set(h.header, "aGVsbG8=")

		headers := collectS3Headers(req)
		if got := headers[checksumHeaderFor(h.algo)]; got != "aGVsbG8=" {
			t.Fatalf("algo %q: header not captured: %v", h.algo, headers)
		}
		if got := headers["x-amz-checksum-algorithm"]; got != h.algo {
			t.Fatalf("algo %q: marker %q", h.algo, got)
		}
	}
}

// BenchmarkParseChecksum measures the hot-path lookup. Pre-canonicalized
// literals keep this at zero allocations per request.
func BenchmarkParseChecksum(b *testing.B) {
	req, err := http.NewRequest(http.MethodPut, "http://momo/obj", nil)
	if err != nil {
		b.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Amz-Checksum-Sha256", "aGVsbG8=")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, perr := parseChecksum(req); perr != nil {
			b.Fatalf("parseChecksum: %v", perr)
		}
	}
}

// BenchmarkParseChecksumDynamicBaseline documents what the dynamic header
// construction cost, so the optimization's benefit stays measurable.
func BenchmarkParseChecksumDynamicBaseline(b *testing.B) {
	req, err := http.NewRequest(http.MethodPut, "http://momo/obj", nil)
	if err != nil {
		b.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Amz-Checksum-Sha256", "aGVsbG8=")
	algos := []string{checksumAlgoCRC32c, checksumAlgoCRC32, checksumAlgoSHA1, checksumAlgoSHA256}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, a := range algos {
			//nolint:staticcheck // S1035: reproducing the old double-canonicalization on purpose
			_ = req.Header.Get(http.CanonicalHeaderKey("x-amz-checksum-" + a))
		}
	}
}
