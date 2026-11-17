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
