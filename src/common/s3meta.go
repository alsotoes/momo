package common

import (
	"encoding/json"
	"fmt"
	"syscall"
)

// MaxS3MetaJSONBytes bounds the serialized size of S3 object metadata (Rule 24).
// 10 headers * 256-byte values plus keys stays well within this cap.
const MaxS3MetaJSONBytes = 8192

// MarshalS3MetaJSON serializes S3 object headers to JSON. Input values are
// bounded and CR/LF-stripped by the transport layer before reaching here; this
// function only errors if the result would exceed the safety cap.
func MarshalS3MetaJSON(headers map[string]string) ([]byte, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(headers)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal S3 headers: %w", err)
	}
	if len(data) > MaxS3MetaJSONBytes {
		return nil, fmt.Errorf("S3 header payload exceeds %d bytes: %w", MaxS3MetaJSONBytes, syscall.EOVERFLOW)
	}
	return data, nil
}

// UnmarshalS3MetaJSON parses a JSON S3-header payload produced by
// MarshalS3MetaJSON. Malformed or oversized payloads yield nil (callers treat
// missing S3 metadata gracefully).
func UnmarshalS3MetaJSON(data []byte) (map[string]string, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) > MaxS3MetaJSONBytes {
		return nil, fmt.Errorf("S3 header payload exceeds %d bytes: %w", MaxS3MetaJSONBytes, syscall.EOVERFLOW)
	}
	var headers map[string]string
	if err := json.Unmarshal(data, &headers); err != nil {
		return nil, fmt.Errorf("failed to unmarshal S3 headers: %w", err)
	}
	return headers, nil
}
