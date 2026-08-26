package storage

import (
	"bytes"
	"errors"
	"io"
	"syscall"
	"testing"
)

// newVerifiedCASStore builds a CASStore with the verifiedCache read policy.
// The blob store and bbolt db share one dir so getBlobPath matches (mirrors
// NewCASStore).
func newVerifiedCASStore(t *testing.T) *CASStore {
	t.Helper()
	dir := t.TempDir()
	blobs, err := NewLocalBlobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := newCASStore(dir, blobs, WithReadVerifier(VerifierVerifiedCache))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestVerifiedCache_FirstReadEstablishesTrustSecondSkips(t *testing.T) {
	// RV-T7
	s := newVerifiedCASStore(t)
	defer s.Close()

	content := []byte("hello verified cache")
	hash := putBlob(t, s, "f", content)

	vc := s.verifier.(*verifiedCache)
	// Empty on construction (RV-T6): nothing trusted before a real verify.
	if vc.isTrusted(hash) {
		t.Fatal("trusted set should be empty on boot")
	}

	rc, _, err := s.Get("f")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatalf("first read (full verify) should succeed: %v", err)
	}
	rc.Close()

	// First-read EOF must have established trust.
	if !vc.isTrusted(hash) {
		t.Fatal("first successful read should mark blob trusted")
	}

	rc, _, err = s.Get("f")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(rc); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("second read should return content, got %q err %v", got, err)
	}
	rc.Close()
}

func TestVerifiedCache_ScrubEstablishesTrust(t *testing.T) {
	// RV-T8
	s := newVerifiedCASStore(t)
	defer s.Close()

	content := []byte("scrub-trusted")
	hash := putBlob(t, s, "f", content)
	vc := s.verifier.(*verifiedCache)

	if err := s.runScrub(); err != nil {
		t.Fatalf("scrub pass failed: %v", err)
	}
	if !vc.isTrusted(hash) {
		t.Fatal("scrub digest match should mark blob trusted")
	}
}

func TestVerifiedCache_NoTrustWithoutVerification(t *testing.T) {
	// RV-T9: a corrupt blob never read or scrubbed must not be trusted, so a
	// fresh read still verifies and fails at EOF.
	s := newVerifiedCASStore(t)
	defer s.Close()

	content := []byte("never-read corrupter")
	hash := putBlob(t, s, "f", content)
	corruptBlob(t, s, hash)

	rc, _, err := s.Get("f")
	if err != nil {
		t.Fatalf("Get should succeed, error surfaced at EOF: %v", err)
	}
	defer rc.Close()
	if _, rerr := io.ReadAll(rc); rerr == nil {
		t.Fatal("corrupt unverified blob should fail at EOF")
	}
}

func TestVerifiedCache_TrustedBlobRotsAndScrubCatches(t *testing.T) {
	// RV-T10
	s := newVerifiedCASStore(t)
	defer s.Close()

	content := []byte("rot-after-trust")
	hash := putBlob(t, s, "f", content)

	// Establish trust via a clean first read.
	rc, _, err := s.Get("f")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatalf("establish-trust read failed: %v", err)
	}
	rc.Close()

	// Physical rot after verification: impossible for the immutable logical
	// object, possible for the physical store.
	corruptBlob(t, s, hash)

	// A later read may skip re-hashing (trusted), so rotation is NOT surfaced
	// on that path — but the background scrub MUST still detect + quarantine it.
	if err := s.runScrub(); err != nil {
		t.Fatalf("scrub pass failed: %v", err)
	}
	if _, _, err := s.Get("f"); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("trusted-then-rotted blob should be quarantined to ENOENT, got: %v", err)
	}
}

func TestReadVerifier_DefaultIsEveryRead(t *testing.T) {
	// RV-T3/RV-T11: no policy override → default = current VerifyOnRead=true
	// behavior (always verify).
	dir := t.TempDir()
	blobs, err := NewLocalBlobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := newCASStore(dir, blobs)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, ok := s.verifier.(everyReadVerifier); !ok {
		t.Fatalf("default verifier should be everyReadVerifier, got %T", s.verifier)
	}

	content := []byte("default-path corrupt")
	hash := putBlob(t, s, "f", content)
	corruptBlob(t, s, hash)

	rc, _, err := s.Get("f")
	if err != nil {
		t.Fatalf("Get should succeed, error surfaced at EOF: %v", err)
	}
	defer rc.Close()
	if _, rerr := io.ReadAll(rc); rerr == nil {
		t.Fatal("default every-read verifier should fail corrupt blob at EOF")
	}
}

func TestVerifiedCache_VerifyOnReadDisabledServesUnchecked(t *testing.T) {
	// RV-T11: the VerifyOnRead bool knob remains backward compatible — off
	// means no verification regardless of policy.
	s := newVerifiedCASStore(t)
	defer s.Close()

	content := []byte("disabled serves corrupt")
	hash := putBlob(t, s, "f", content)
	corruptBlob(t, s, hash)
	s.VerifyOnRead = false

	rc, _, err := s.Get("f")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatalf("VerifyOnRead=false should not check hash, got: %v", err)
	}
}

func TestWithReadVerifier_UnknownNamePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("unknown verifier name should panic (fail-closed)")
		}
	}()
	WithReadVerifier("bogus")(&CASStore{})
}
