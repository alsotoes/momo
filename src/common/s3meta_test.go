package common

import (
	"errors"
	"strings"
	"syscall"
	"testing"
)

func TestS3MetaJSONRoundTrip(t *testing.T) {
	headers := map[string]string{
		"content-type":     "image/png",
		"x-amz-meta-user":  "alice",
		"cache-control":    "max-age=3600",
		"content-encoding": "gzip",
	}
	data, err := MarshalS3MetaJSON(headers)
	if err != nil {
		t.Fatalf("MarshalS3MetaJSON failed: %v", err)
	}
	got, err := UnmarshalS3MetaJSON(data)
	if err != nil {
		t.Fatalf("UnmarshalS3MetaJSON failed: %v", err)
	}
	if len(got) != len(headers) {
		t.Fatalf("round-trip key count mismatch: got %d, want %d", len(got), len(headers))
	}
	for k, v := range headers {
		if got[k] != v {
			t.Errorf("round-trip mismatch for %q: got %q, want %q", k, got[k], v)
		}
	}
}

func TestS3MetaJSONEmptyAndNil(t *testing.T) {
	if data, err := MarshalS3MetaJSON(nil); err != nil || data != nil {
		t.Errorf("MarshalS3MetaJSON(nil) = %v, %v; want nil, nil", data, err)
	}
	headers, err := UnmarshalS3MetaJSON(nil)
	if err != nil || headers != nil {
		t.Errorf("UnmarshalS3MetaJSON(nil) = %v, %v; want nil, nil", headers, err)
	}
	headers, err = UnmarshalS3MetaJSON([]byte("not-json{"))
	if err == nil || headers != nil {
		t.Errorf("UnmarshalS3MetaJSON(malformed) = %v, %v; want error, nil", headers, err)
	}
}

func TestS3MetaJSONOversizeRejected(t *testing.T) {
	// An oversized payload must be rejected by the 8192-byte cap (Rule 24).
	big := map[string]string{
		"x-amz-meta-big": strings.Repeat("a", MaxS3MetaJSONBytes*2),
	}
	if _, err := MarshalS3MetaJSON(big); err == nil {
		t.Error("MarshalS3MetaJSON(oversize) should reject the payload")
	} else if !errors.Is(err, syscall.EOVERFLOW) {
		t.Errorf("expected EOVERFLOW, got: %v", err)
	}

	data := []byte(strings.Repeat("a", MaxS3MetaJSONBytes*2))
	if _, err := UnmarshalS3MetaJSON(data); err == nil {
		t.Error("UnmarshalS3MetaJSON(oversize) should reject the payload")
	}
}
