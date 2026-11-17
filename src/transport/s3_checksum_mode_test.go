package transport

import (
	"net/http"
	"strings"
	"testing"
)

// TestAppendS3MetaHeadersGatesChecksums pins S3 checksum-mode semantics: the
// response carries x-amz-checksum-* only when the request asked for it with
// x-amz-checksum-mode: ENABLED. Echoing it unsolicited made a modern S3 client
// validate the multipart composite checksum against the downloaded bytes and
// fail with "response checksum header does not match calculated checksum"
// (issue #1352).
func TestAppendS3MetaHeadersGatesChecksums(t *testing.T) {
	headers := map[string]string{
		"content-type":             "application/octet-stream",
		"x-amz-checksum-crc32":     "AAAAAA==",
		"x-amz-checksum-algorithm": "CRC32",
		"x-amz-meta-owner":         "team-a",
		"cache-control":            "max-age=60",
	}

	plain := string(appendS3MetaHeaders(nil, headers, "", false))
	if strings.Contains(plain, "x-amz-checksum-") {
		t.Fatalf("plain response must not carry checksum headers, got %q", plain)
	}
	for _, want := range []string{"Content-Type: application/octet-stream", "x-amz-meta-owner: team-a", "cache-control: max-age=60"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("plain response lost %q: %q", want, plain)
		}
	}

	withMode := string(appendS3MetaHeaders(nil, headers, "", true))
	if !strings.Contains(withMode, "x-amz-checksum-crc32: AAAAAA==") {
		t.Fatalf("checksum-mode response must carry the stored checksum, got %q", withMode)
	}
}

// TestChecksumModeEnabled pins the request-header check.
func TestChecksumModeEnabled(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:4440/b/k", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if checksumModeEnabled(req) {
		t.Fatal("checksum mode must be off by default")
	}
	req.Header.Set("X-Amz-Checksum-Mode", "ENABLED")
	if !checksumModeEnabled(req) {
		t.Fatal("checksum mode must be detected (case-insensitive header)")
	}
}

// TestCompositeChecksumDetection pins the multipart checksum rule: a value with
// a "-<partCount>" suffix is a checksum of the parts, not of the object (#1354).
func TestCompositeChecksumDetection(t *testing.T) {
	cases := map[string]bool{
		"AAAAAA==":           false,
		"abc==-4":            true,
		"abc==-12":           true,
		"":                   false,
		"-3":                 false,
		"abc==-":             false,
		"abc==-x":            false,
		"dGVzdC1vYmplY3Q=-1": true,
	}
	for value, want := range cases {
		if got := isCompositeChecksum(value); got != want {
			t.Fatalf("isCompositeChecksum(%q) = %v, want %v", value, got, want)
		}
	}
}

// TestCompositeChecksumIsNotEchoed proves a composite value is dropped from the
// response even in checksum mode, so a client cannot validate it against the
// object bytes.
func TestCompositeChecksumIsNotEchoed(t *testing.T) {
	headers := map[string]string{
		"x-amz-checksum-crc32":     "abc==-4",
		"x-amz-checksum-algorithm": "CRC32",
	}
	got := string(appendS3MetaHeaders(nil, headers, "", true))
	if strings.Contains(got, "abc==-4") {
		t.Fatalf("composite checksum echoed to the client: %q", got)
	}

	whole := map[string]string{"x-amz-checksum-crc32": "AAAAAA=="}
	if out := string(appendS3MetaHeaders(nil, whole, "", true)); !strings.Contains(out, "CCCCCC") && !strings.Contains(out, "AAAAAA==") {
		t.Fatalf("whole-object checksum must still be echoed, got %q", out)
	}
}

// TestTrailerChecksumSelection pins how a trailing checksum is located: a trailer
// value wins, then the declared X-Amz-Trailer, then none.
func TestTrailerChecksumSelection(t *testing.T) {
	name, value, ok := trailerChecksum(map[string]string{"x-amz-checksum-crc32": "AAAAAA=="})
	if !ok || name != "x-amz-checksum-crc32" || value != "AAAAAA==" {
		t.Fatalf("got (%q,%q,%v)", name, value, ok)
	}

	if _, _, ok = trailerChecksum(nil); ok {
		t.Fatal("no trailer must report ok=false")
	}
	if _, _, ok = trailerChecksum(map[string]string{"x-amz-meta-note": "v"}); ok {
		t.Fatal("a non-checksum trailer must report ok=false")
	}
	// Declared but never delivered: nothing to verify, not an error.
	if _, _, ok = trailerChecksum(map[string]string{"x-amz-checksum-crc32": ""}); ok {
		t.Fatal("an empty trailer value must report ok=false")
	}
}

// TestIsTrailerName pins the header-name character set, so a trailer cannot
// smuggle arbitrary bytes into a log line or a response header.
func TestIsTrailerName(t *testing.T) {
	for _, good := range []string{"x-amz-checksum-crc32", "x_amz_1"} {
		if !isTrailerName(good) {
			t.Fatalf("%q must be accepted", good)
		}
	}
	for _, bad := range []string{"", "x y", "x:y", "x\x00", strings.Repeat("a", 129)} {
		if isTrailerName(bad) {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}
