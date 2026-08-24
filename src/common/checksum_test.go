package common

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"strings"
	"testing"
)

func sha256Ref(b []byte) ChecksumRef {
	sum := sha256.Sum256(b)
	return ChecksumRef{Algorithm: ChecksumSHA256, Value: base64.StdEncoding.EncodeToString(sum[:])}
}

func TestChecksumSet_VerifiedMatch(t *testing.T) {
	body := []byte("hello integrity")
	s := NewChecksumSetFromRefs([]ChecksumRef{sha256Ref(body)})
	if _, err := s.Write(body); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !s.Verified([]ChecksumRef{sha256Ref(body)}) {
		t.Fatal("expected checksum to verify")
	}
}

func TestChecksumSet_Mismatch(t *testing.T) {
	body := []byte("hello integrity")
	bad := ChecksumRef{Algorithm: ChecksumSHA256, Value: base64.StdEncoding.EncodeToString([]byte("wrong"))}
	s := NewChecksumSetFromRefs([]ChecksumRef{bad})
	if _, err := s.Write(body); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if s.Verified([]ChecksumRef{bad}) {
		t.Fatal("expected mismatch to fail verification")
	}
}

func TestVerifyStream_NoChecksumsInert(t *testing.T) {
	if err := VerifyStream(strings.NewReader("x"), nil); err != nil {
		t.Fatalf("expected nil for no checksums, got %v", err)
	}
}

func TestVerifyStream_Match(t *testing.T) {
	body := []byte("hello integrity")
	if err := VerifyStream(bytes.NewReader(body), []ChecksumRef{sha256Ref(body)}); err != nil {
		t.Fatalf("expected match, got %v", err)
	}
}

func TestVerifyStream_Mismatch(t *testing.T) {
	bad := ChecksumRef{Algorithm: ChecksumSHA256, Value: base64.StdEncoding.EncodeToString([]byte("nope"))}
	if err := VerifyStream(strings.NewReader("hello"), []ChecksumRef{bad}); err == nil {
		t.Fatal("expected mismatch error")
	}
}

var _ io.Writer = (*ChecksumSet)(nil)
