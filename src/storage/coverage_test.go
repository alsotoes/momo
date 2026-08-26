package storage

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"go.etcd.io/bbolt"
)

// everyReadVerifier.MarkTrusted is a structural no-op but must never panic.
func TestEveryReadVerifier_MarkTrustedNoop(t *testing.T) {
	e := everyReadVerifier{}
	e.MarkTrusted("hash")
	// default policy always verifies regardless of MarkTrusted.
	r := e.Verify(io.NopCloser(bytes.NewReader(nil)), "hash")
	if _, ok := r.(*verifyingReadCloser); !ok {
		t.Fatalf("everyReadVerifier.Verify should wrap in verifyingReadCloser, got %T", r)
	}
}

func TestDefaultScrubConfig(t *testing.T) {
	if got := DefaultScrubConfig(); got.Interval != time.Hour {
		t.Fatalf("default scrub interval should be 1h, got %v", got)
	}
}

// default constructor + Close roundtrips with the everyReadVerifier policy.
func TestNewCASStore_DefaultPolicyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewCASStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.verifier.(everyReadVerifier); !ok {
		t.Fatalf("default policy should be everyReadVerifier, got %T", s.verifier)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close should be idempotent-safe: %v", err)
	}
}

// newCASStore surfaces I/O errors when the data directory is unusable.
func TestNewCASStore_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	blobs, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer blobs.Close()
	// Replace the data path with a regular file so MkdirAll fails (EIO).
	file := filepath.Join(dir, "blocker")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newCASStore(file, blobs); err == nil {
		t.Fatal("expected mkdir/io error when data dir is a file")
	}
}

// Put rejects path-traversal hashes (EINVAL) and over-long names (ENAMETOOLONG).
func TestPut_ValidationErrors(t *testing.T) {
	dir := t.TempDir()
	s, err := NewCASStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Put("f", "../evil", 1, "", bytes.NewReader([]byte("x"))); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("expected EINVAL for traversal hash, got %v", err)
	}
	long := make([]byte, common.FileInfoLength+1)
	for i := range long {
		long[i] = 'n'
	}
	if err := s.Put(string(long), "hash", 1, "", bytes.NewReader([]byte("x"))); !errors.Is(err, syscall.ENAMETOOLONG) {
		t.Fatalf("expected ENAMETOOLONG for over-long name, got %v", err)
	}
	if _, err := s.Has("../evil"); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("expected EINVAL for traversal hash in Has, got %v", err)
	}
}

// PutS3Meta / GetS3Meta roundtrip and treat empty headers as no-op.
func TestS3Meta_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewCASStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	putBlob(t, s, "f", []byte("payload"))
	if err := s.PutS3Meta("f", nil); err != nil {
		t.Fatalf("empty headers should no-op: %v", err)
	}
	headers := map[string]string{"Content-Type": "application/octet-stream", "x-amz-meta-x": "y"}
	if err := s.PutS3Meta("f", headers); err != nil {
		t.Fatalf("PutS3Meta failed: %v", err)
	}
	got := s.GetS3Meta("f")
	if got["Content-Type"] != "application/octet-stream" || got["x-amz-meta-x"] != "y" {
		t.Fatalf("roundtripped S3 meta mismatch: %v", got)
	}
	if got := s.GetS3Meta("missing"); len(got) != 0 {
		t.Fatalf("metadata for missing name should be empty, got %v", got)
	}
}

// isLocalBackend recognizes the encrypted-over-local wrapping.
func TestIsLocalBackend_EncryptedLocal(t *testing.T) {
	dir := t.TempDir()
	inner, err := NewLocalBlobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := NewEncryptedBlobStore(inner, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	s, err := newCASStore(dir, enc)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.isLocalBackend() {
		t.Fatal("encrypted-over-local should be a local backend")
	}
}

// GetMeta surfaces ENOENT for tombstoned / unknown names and EBADMSG for a
// stored object whose size is negative (corrupt metadata), mirroring Get.
func TestGetMeta_ErrorPaths(t *testing.T) {
	dir := t.TempDir()
	s, err := NewCASStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.GetMeta("missing"); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("unknown name should be ENOENT, got %v", err)
	}

	putBlob(t, s, "f", []byte("x"))
	if err := s.Delete("f"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMeta("f"); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("tombstoned name should be ENOENT, got %v", err)
	}

	// Insert a negative-size object meta for a live name (corrupt at-rest
	// metadata) and confirm GetMeta refuses it.
	putBlob(t, s, "g", []byte("x2"))
	neg := ObjectMeta{Size: -1, RefCount: 1}.encode()
	err = s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketObjects).Put([]byte(common.HashBytes([]byte("x2"))), neg)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMeta("g"); err == nil {
		t.Fatal("negative-size object metadata should return an error from GetMeta")
	}
}

// errBlobStore fails GetBlob for a hash so scrub/read error surfaces propagate
// through runScrub (the deferred-close path) instead of being swallowed.
type errBlobStore struct {
	*LocalBlobStore
}

func (e errBlobStore) GetBlob(hash string) (io.ReadCloser, error) {
	return nil, syscall.EIO
}

// scrub surfaces a get error and SafeClose is exercised via the store Close.
func TestScrub_GetBlobErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	inner, err := NewLocalBlobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Close()
	eb := errBlobStore{LocalBlobStore: inner}
	s, err := newCASStore(dir, eb)
	if err != nil {
		t.Fatal(err)
	}

	// referencedBlobs needs a referenced object, so seed metadata directly.
	h := common.HashBytes([]byte("x"))
	err = s.db.Update(func(tx *bbolt.Tx) error {
		meta := ObjectMeta{Size: 1, RefCount: 1}
		if err := tx.Bucket(bucketObjects).Put([]byte(h), meta.encode()); err != nil {
			return err
		}
		return tx.Bucket(bucketNamespace).Put([]byte("x"), []byte(h))
	})
	if err != nil {
		t.Fatal(err)
	}

	// runScrub logs-and-continues on a GetBlob failure (no abort).
	if err := s.runScrub(); err != nil {
		t.Fatalf("runScrub should continue past a GetBlob failure, got %v", err)
	}
	// scrubBlob itself surfaces the EIO blocker.
	if _, _, err := s.scrubBlob(h); !errors.Is(err, syscall.EIO) {
		t.Fatalf("scrubBlob should surface GetBlob EIO, got %v", err)
	}
	s.Close()
}

// runScrub returns early when the store is being closed mid-pass (canceled).
func TestScrub_ClosedReturnsEarly(t *testing.T) {
	dir := t.TempDir()
	s, err := NewCASStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, s, "a", []byte("aaaa"))
	// Signal close so scrubBlob's canceled branch returns before verifying.
	close(s.scrubDone)
	// Close re-closes the channel-guarded resources; scrub returns nils.
	if err := s.runScrub(); err != nil {
		t.Fatalf("runScrub when canceled should return nil, got %v", err)
	}
}
