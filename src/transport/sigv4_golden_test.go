package transport

import (
	"net/http"
	"sort"
	"strings"
	"syscall"
	"testing"
)

// legacyBuildCanonicalHeaders is a verbatim copy of the pre-optimization
// implementation (master, before the ⚡ Bolt zero-allocation rewrite). It exists
// ONLY as the oracle for the golden equivalence test below: SigV4
// canonicalization is auth-critical, so the optimized version must produce
// byte-identical output for every realistic input.
func legacyBuildCanonicalHeaders(req *http.Request, signedHeadersStr string) string {
	headerNames := strings.Split(signedHeadersStr, ";")
	sort.Strings(headerNames)

	var sb strings.Builder
	for _, h := range headerNames {
		sb.WriteString(h)
		sb.WriteByte(':')
		val := req.Header.Get(h)
		if h == "host" {
			val = req.Host
		}
		val = strings.TrimSpace(val)
		sb.WriteString(val)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func goldenRequest() *http.Request {
	r := &http.Request{
		Method: "PUT",
		Host:   "s3.example.com:9000",
		Header: http.Header{},
	}
	r.Header.Set("X-Amz-Date", "20260101T000000Z")
	r.Header.Set("X-Amz-Content-Sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Add("X-Amz-Meta-Multi", "first")
	r.Header.Add("X-Amz-Meta-Multi", "second")
	r.Header.Set("X-Empty", "")
	r.Header.Set("X-Spaced", "  padded value  ")
	return r
}

// TestBuildCanonicalHeaders_GoldenEquivalence asserts the optimized
// implementation is byte-identical to the legacy one for every realistic input.
func TestBuildCanonicalHeaders_GoldenEquivalence(t *testing.T) {
	cases := []struct {
		name          string
		signedHeaders string
	}{
		{"typical_s3", "host;x-amz-content-sha256;x-amz-date"},
		{"single_host", "host"},
		{"unsorted", "x-amz-date;host;content-type"},
		{"multi_value_header", "host;x-amz-meta-multi"},
		{"empty_value_header", "host;x-empty"},
		{"spaced_value", "host;x-spaced"},
		{"all_present", "content-type;host;x-amz-content-sha256;x-amz-date;x-amz-meta-multi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := goldenRequest()
			want := legacyBuildCanonicalHeaders(req, tc.signedHeaders)
			got, err := buildCanonicalHeaders(req, tc.signedHeaders)
			if err != nil {
				t.Fatalf("optimized returned error %v; legacy produced %q", err, want)
			}
			if got != want {
				t.Fatalf("canonical output diverged:\n  legacy: %q\n  optimized: %q", want, got)
			}
		})
	}
}

// TestBuildCanonicalHeaders_NonLowercaseHost documents why a non-conformant
// signedHeaders list (AWS mandates lowercase) is NOT in the byte-equivalence
// set: the legacy implementation emitted an empty host value (h == "host" only
// matched the exact lowercase form), which is an undefined/broken canonical
// request. The optimized version fails closed instead.
func TestBuildCanonicalHeaders_NonLowercaseHost(t *testing.T) {
	req := goldenRequest()
	legacy := legacyBuildCanonicalHeaders(req, "Host;X-Amz-Date")
	if !strings.HasPrefix(legacy, "Host:\n") {
		t.Fatalf("legacy behavior changed; expected an empty host value, got %q", legacy)
	}
	if _, err := buildCanonicalHeaders(req, "Host;X-Amz-Date"); err == nil {
		t.Fatal("expected the optimized version to fail closed on a non-lowercase host entry")
	}
}

// TestBuildCanonicalHeaders_MissingHeader documents the one intentional
// behavioral change: a signed header that is absent from the request is now
// rejected (EBADMSG) instead of silently contributing an empty value. Both
// paths fail authentication; the new path fails closed and explicitly.
func TestBuildCanonicalHeaders_MissingHeader(t *testing.T) {
	req := goldenRequest()
	_, err := buildCanonicalHeaders(req, "host;x-absent")
	if err == nil {
		t.Fatal("expected EBADMSG for an absent signed header")
	}
	if !strings.Contains(err.Error(), syscall.EBADMSG.Error()) {
		t.Fatalf("expected EBADMSG, got %v", err)
	}
}

// TestBuildCanonicalHeaders_EmptySegmentsSkipped documents the second
// intentional change: empty segments from malformed signedHeaders lists are
// skipped rather than emitting a stray ":\\n" entry.
func TestBuildCanonicalHeaders_EmptySegmentsSkipped(t *testing.T) {
	req := goldenRequest()
	got, err := buildCanonicalHeaders(req, "host;;x-amz-date;")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "host:s3.example.com:9000\nx-amz-date:20260101T000000Z\n"
	if got != want {
		t.Fatalf("empty segments not skipped:\n  want %q\n  got  %q", want, got)
	}
}

// TestBuildCanonicalHeaders_HeaderCountBound asserts the Sentinel bound
// (Rule 32): an oversized signedHeaders list fails closed rather than
// overflowing the stack buffer.
func TestBuildCanonicalHeaders_HeaderCountBound(t *testing.T) {
	req := goldenRequest()
	tooMany := strings.Repeat("a;", 64) + "z"
	if _, err := buildCanonicalHeaders(req, tooMany); err == nil {
		t.Fatal("expected failure when signedHeaders exceeds the buffer capacity")
	}
}

func TestBuildCanonicalHeaders_HeaderValueLengthBound(t *testing.T) {
	req := goldenRequest()
	req.Header.Set("x-amz-oversized", strings.Repeat("a", 4097))
	if _, err := buildCanonicalHeaders(req, "host;x-amz-oversized"); err == nil {
		t.Fatal("expected failure when a canonical header value exceeds 4096 bytes")
	}
}

func TestGetCanonicalHeaderValue_PanicRecovery(t *testing.T) {
	_, err := getCanonicalHeaderValue(nil, "host")
	if err != syscall.EIO {
		t.Fatalf("expected EIO on panic, got: %v", err)
	}
}
