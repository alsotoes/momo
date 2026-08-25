package storage

import (
	"bytes"
	"errors"
	"io"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

// putBlob stores a named object and returns its content hash.
func putBlob(t *testing.T, s *CASStore, name string, content []byte) string {
	t.Helper()
	hash := common.HashBytes(content)
	if err := s.Put(name, hash, int64(len(content)), "", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	return hash
}

// corruptBlob overwrites the on-disk blob for a hash so it no longer matches.
func corruptBlob(t *testing.T, s *CASStore, hash string) {
	t.Helper()
	p := s.getBlobPath(hash)
	f, err := os.OpenFile(p, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open blob for corruption: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteAt([]byte{0xBB}, 0); err != nil {
		t.Fatalf("corrupt blob: %v", err)
	}
}

func TestVerifyOnRead_ValidBlobServesToEOF(t *testing.T) {
	dir := t.TempDir()
	s, err := NewCASStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	content := []byte("hello integrity")
	putBlob(t, s, "f", content)

	rc, _, err := s.Get("f")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatalf("valid blob should read to EOF without error, got: %v", err)
	}
}

func TestVerifyOnRead_CorruptBlobFailsAtEOF(t *testing.T) {
	dir := t.TempDir()
	s, err := NewCASStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	content := []byte("hello integrity")
	hash := putBlob(t, s, "f", content)
	corruptBlob(t, s, hash)

	rc, _, err := s.Get("f")
	if err != nil {
		t.Fatalf("Get should succeed (error only surfaces at EOF): %v", err)
	}
	defer rc.Close()
	_, rerr := io.ReadAll(rc)
	if rerr == nil {
		t.Fatal("expected integrity error reading corrupted blob, got nil")
	}
	if !errors.Is(rerr, common.ErrIntegrityMismatch) {
		t.Errorf("expected ErrIntegrityMismatch, got: %v", rerr)
	}
	if !errors.Is(rerr, syscall.EBADMSG) {
		t.Errorf("expected syscall.EBADMSG, got: %v", rerr)
	}
}

func TestVerifyOnRead_DisabledServesCorruptBytes(t *testing.T) {
	dir := t.TempDir()
	s, err := NewCASStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	content := []byte("hello integrity")
	hash := putBlob(t, s, "f", content)
	corruptBlob(t, s, hash)
	s.VerifyOnRead = false

	rc, _, err := s.Get("f")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatalf("verify_on_read=false should not check hash, got: %v", err)
	}
}

func TestScrub_HealthyStoreRoundTrips(t *testing.T) {
	dir := t.TempDir()
	s, err := NewCASStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	putBlob(t, s, "a", []byte("aaaa"))
	putBlob(t, s, "b", []byte("bbbb"))

	if err := s.runScrub(); err != nil {
		t.Fatalf("scrub pass failed: %v", err)
	}

	for _, name := range []string{"a", "b"} {
		rc, _, err := s.Get(name)
		if err != nil {
			t.Fatalf("read %s after scrub: %v", name, err)
		}
		io.Copy(io.Discard, rc)
		rc.Close()
	}
}

func TestScrub_QuarantinesCorruptBlob(t *testing.T) {
	dir := t.TempDir()
	s, err := NewCASStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	content := []byte("hello scrub")
	hash := putBlob(t, s, "f", content)
	corruptBlob(t, s, hash)

	if err := s.runScrub(); err != nil {
		t.Fatalf("scrub pass failed: %v", err)
	}

	if _, _, err := s.Get("f"); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("read of quarantined blob should fail ENOENT, got: %v", err)
	}
	// Metadata for the blob should also be gone.
	hashed, err := s.Has(hash)
	if err != nil {
		t.Fatal(err)
	}
	if hashed {
		t.Error("quarantined blob metadata should have been removed")
	}
}

func TestScrub_StartStopGoroutine(t *testing.T) {
	goleak.VerifyNone(t) // runs after the deferred Close below (LIFO)
	dir := t.TempDir()
	s, err := NewCASStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.StartScrub(ScrubConfig{Interval: 50 * time.Millisecond})
	waitScrubStarted(t, s)
}

func TestScrub_StartScrubIdempotent(t *testing.T) {
	goleak.VerifyNone(t)
	dir := t.TempDir()
	s, err := NewCASStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.StartScrub(ScrubConfig{Interval: time.Hour})
	s.StartScrub(ScrubConfig{Interval: time.Hour})
	waitScrubStarted(t, s) // single goroutine; sync.Once prevents a second
}

// waitScrubStarted polls until the scrub goroutine has observed its start flag,
// avoiding a race between StartScrub and the immediate flag read.
func waitScrubStarted(t *testing.T, s *CASStore) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for s.scrubStarted.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.scrubStarted.Load() != 1 {
		t.Fatal("scrub goroutine did not report started")
	}
}
