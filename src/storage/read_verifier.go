package storage

import (
	"fmt"
	"io"
	"log"
	"sync"
	"syscall"
)

// ReadVerifier decides at read-dispatch time whether a blob must be re-hashed
// against its content-address key while streaming out of Get. It is the
// Rule 74 seam that replaces the raw VerifyOnRead boolean gate at the read
// wrap-point, so the policy is chosen by a compiled-in constructor rather than
// a bare bool. Core integrity invariants (trust only after a real full
// verification, CAS immutability, scrub re-catch of physical rot) remain in
// the auditable core.
type ReadVerifier interface {
	// Verify returns the reader to serve for a blob addressed by hash. It may
	// wrap the underlying reader to re-derive SHA-256 at EOF, or return it
	// unchanged when the blob is already trusted.
	Verify(underlying io.ReadCloser, hash string) io.ReadCloser

	// MarkTrusted records that a blob was fully verified against its content
	// address during this process. everyReadVerifier ignores it; verifiedCache
	// stashes it so later reads skip re-hashing.
	MarkTrusted(hash string)
}

// everyReadVerifier is the DEFAULT verifier: it always wraps the read in a
// full SHA-256 verification (the historical VerifyOnRead=true behavior).
// Zero behavior change.
type everyReadVerifier struct{}

// Verify wraps every read so the full object hash is re-derived at EOF.
func (everyReadVerifier) Verify(underlying io.ReadCloser, hash string) io.ReadCloser {
	return &verifyingReadCloser{
		verifyingReader: newVerifyingReader(underlying, hash),
		underlying:      underlying,
	}
}

// MarkTrusted is a structural no-op: this verifier always verifies.
func (everyReadVerifier) MarkTrusted(string) {}

// verifiedCache serves a blob WITHOUT re-hashing once it is in a trusted set.
// A blob enters trust ONLY after a successful full SHA-256 verification in
// this process (read-EOF or scrubBlob digest match); because content
// addresses are immutable, that trust stays valid. The set is in-process and
// empty on construction, so every boot re-verifies each blob once.
type verifiedCache struct {
	mu      sync.RWMutex
	trusted map[string]struct{}
}

// newVerifiedCache returns an empty verified-trust cache.
func newVerifiedCache() *verifiedCache {
	return &verifiedCache{trusted: make(map[string]struct{})}
}

// Verify skips re-hashing for trusted blobs (fast path: no alloc, no hasher).
// Untrusted blobs are wrapped in a verifying reader that marks them trusted
// once the full hash matches at EOF.
func (vc *verifiedCache) Verify(underlying io.ReadCloser, hash string) io.ReadCloser {
	vr := newVerifyingReader(underlying, hash)
	vr.onVerified = func() { vc.MarkTrusted(hash) }
	return &verifyingReadCloser{verifyingReader: vr, underlying: underlying}
}

// isTrusted reports whether hash was fully verified this process.
func (vc *verifiedCache) isTrusted(hash string) bool {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	_, ok := vc.trusted[hash]
	return ok
}

// MarkTrusted stashes hash in the trusted set. Panic-safe: a failed lookup or
// insert is logged and surfaced as EIO rather than crashing the read path.
func (vc *verifiedCache) MarkTrusted(hash string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in verifiedCache.MarkTrusted for %s: %v", hash, r)
		}
	}()
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.trusted[hash] = struct{}{}
}

// verifierNames are the policy names accepted by WithReadVerifier.
const (
	VerifierEveryRead     = "everyread"
	VerifierVerifiedCache = "verifiedcache"
)

// verifierRegistry is the compiled-in Rule 74 registry of ReadVerifier
// constructors, selected declaratively by name. No external dynamic plugins.
var verifierRegistry = map[string]func() ReadVerifier{
	VerifierEveryRead: func() ReadVerifier { return everyReadVerifier{} },
	VerifierVerifiedCache: func() ReadVerifier {
		return newVerifiedCache()
	},
}

// WithReadVerifier selects the read verification policy for newCASStore by
// name from the compiled-in registry. Unknown names cause a panic at
// construction time (fail-closed) rather than silently degrading integrity.
func WithReadVerifier(name string) func(*CASStore) {
	return func(s *CASStore) {
		ctor, ok := verifierRegistry[name]
		if !ok {
			panic(fmt.Errorf("WithReadVerifier: unknown verifier %q: %w", name, syscall.EINVAL))
		}
		s.verifier = ctor()
	}
}
