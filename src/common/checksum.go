package common

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"syscall"
)

// ChecksumRef names an additive integrity checksum carried alongside the
// authoritative SHA-256 content Hash (issue #903). Protocol-agnostic: any
// surface (s3-*, momo-tcp, momo-quic) maps its client checksums onto this.
// Algorithm is lowercase (crc32/crc32c/sha1/sha256); Value is the base64-encoded
// digest. It is additive only — never the content-address (Hash stays that).
type ChecksumRef struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

const (
	ChecksumCRC32c = "crc32c"
	ChecksumCRC32  = "crc32"
	ChecksumSHA1   = "sha1"
	ChecksumSHA256 = "sha256"
)

// ChecksumHasher returns a fresh digest for a supported checksum algorithm, or
// nil when algo is not supported.
func ChecksumHasher(algo string) hash.Hash {
	switch algo {
	case ChecksumCRC32c:
		return crc32.New(crc32.MakeTable(crc32.Castagnoli))
	case ChecksumCRC32:
		return crc32.NewIEEE()
	case ChecksumSHA1:
		return sha1.New()
	case ChecksumSHA256:
		return sha256.New()
	default:
		return nil
	}
}

// ChecksumSet accumulates digests for a set of algorithms over a single byte
// stream and verifies them at the end. It is the protocol-agnostic streaming
// verifier shared by the ingest path (getFile) and, opt-in, the retrieval path.
// It implements io.Writer and is driven from a single goroutine.
type ChecksumSet struct {
	hashers map[string]hash.Hash
	order   []string
}

// NewChecksumSet builds a set for the given algorithms (unsupported ones are
// skipped).
func NewChecksumSet(algos []string) *ChecksumSet {
	s := &ChecksumSet{hashers: make(map[string]hash.Hash, len(algos))}
	for _, a := range algos {
		if h := ChecksumHasher(a); h != nil {
			s.hashers[a] = h
			s.order = append(s.order, a)
		}
	}
	return s
}

// NewChecksumSetFromRefs builds a set for the algorithms named by refs.
func NewChecksumSetFromRefs(refs []ChecksumRef) *ChecksumSet {
	algos := make([]string, 0, len(refs))
	for _, r := range refs {
		algos = append(algos, r.Algorithm)
	}
	return NewChecksumSet(algos)
}

// Write feeds p to every hasher in the set (no-op when the set is empty).
func (s *ChecksumSet) Write(p []byte) (int, error) {
	for _, a := range s.order {
		s.hashers[a].Write(p)
	}
	return len(p), nil
}

// Results returns the computed base64 digests keyed by algorithm.
func (s *ChecksumSet) Results() map[string]string {
	out := make(map[string]string, len(s.order))
	for _, a := range s.order {
		out[a] = base64.StdEncoding.EncodeToString(s.hashers[a].Sum(nil))
	}
	return out
}

// Verified reports whether the computed digests all match the expected refs.
func (s *ChecksumSet) Verified(expected []ChecksumRef) bool {
	values := s.Results()
	for _, cs := range expected {
		if got, ok := values[cs.Algorithm]; !ok || got != cs.Value {
			return false
		}
	}
	return true
}

// ErrIntegrityMismatch is returned when an additive checksum does not match.
// getFile maps it to a POSIX-mapped error; surfaces may also emit their own
// client error (e.g. S3 writes 400 BadDigest).
var ErrIntegrityMismatch = errors.New("integrity checksum mismatch")

// VerifyStream computes and verifies the additive checksums over r, consuming
// r. Returns nil when no checksums are supplied or all match; otherwise wraps
// ErrIntegrityMismatch with syscall.EBADMSG. Used for opt-in retrieval bit-rot
// checking. Bounded-memory: it streams rather than buffering.
func VerifyStream(r io.Reader, expected []ChecksumRef) error {
	if len(expected) == 0 {
		return nil
	}
	s := NewChecksumSetFromRefs(expected)
	if len(s.order) == 0 {
		return nil
	}
	if _, err := io.Copy(s, r); err != nil {
		return fmt.Errorf("failed to checksum stream: %w", err)
	}
	if !s.Verified(expected) {
		return fmt.Errorf("%w: %s", ErrIntegrityMismatch, syscall.EBADMSG)
	}
	return nil
}
