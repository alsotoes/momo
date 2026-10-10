package storage

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
)

// errReader fails after delivering n bytes, to exercise read-error paths.
type errReader struct{ n int }

func (e *errReader) Read(p []byte) (int, error) {
	if e.n <= 0 {
		return 0, errors.New("boom")
	}
	if len(p) > e.n {
		p = p[:e.n]
	}
	e.n -= len(p)
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestVolumeBlobStore_PutBlobHashMismatch(t *testing.T) {
	vbs, cleanup := newTestVolumeStore(t)
	defer cleanup()

	payload := []byte("hello world")
	hash := contentHash([]byte("different content"))

	if err := vbs.PutBlob(hash, bytes.NewReader(payload)); err == nil {
		t.Fatal("expected hash mismatch error")
	}
}

func TestVolumeBlobStore_PutBlobInvalidHash(t *testing.T) {
	vbs, cleanup := newTestVolumeStore(t)
	defer cleanup()

	payload := []byte("hello")
	good := contentHash(payload)

	// Non-hex hash.
	if err := vbs.PutBlob("zzzz", bytes.NewReader(payload)); err == nil {
		t.Fatal("expected invalid-hash error for non-hex input")
	}
	// Wrong length.
	if err := vbs.PutBlob(good[:62], bytes.NewReader(payload)); err == nil {
		t.Fatal("expected invalid-hash error for short hash")
	}
	// Path traversal.
	if err := vbs.PutBlob("../evil", bytes.NewReader(payload)); err == nil {
		t.Fatal("expected path-traversal rejection")
	}
	// Failing reader.
	if err := vbs.PutBlob(good, &errReader{n: 2}); err == nil {
		t.Fatal("expected read error")
	}
}

func TestVolumeBlobStore_PutBlobNoActiveVolume(t *testing.T) {
	vbs, cleanup := newTestVolumeStore(t)
	defer cleanup()

	vbs.mu.Lock()
	vbs.activeVolume = nil
	vbs.mu.Unlock()

	payload := []byte("hello")
	if err := vbs.PutBlob(contentHash(payload), bytes.NewReader(payload)); err == nil {
		t.Fatal("expected error when no active volume")
	}
}

func TestVolumeBlobStore_PutBlobNeedleLargerThanVolume(t *testing.T) {
	vbs, cleanup := newTestVolumeStore(t)
	defer cleanup()

	// A needle bigger than a whole volume can never fit, even after a roll.
	payload := make([]byte, 4096)
	vbs.mu.Lock()
	vbs.volumeSize = 64
	vbs.mu.Unlock()

	if err := vbs.PutBlob(contentHash(payload), bytes.NewReader(payload)); err == nil {
		t.Fatal("expected ENOSPC")
	} else if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("expected ENOSPC, got %v", err)
	}
}

func TestVolumeBlobStore_PutBlobWriteFailure(t *testing.T) {
	vbs, cleanup := newTestVolumeStore(t)
	defer cleanup()

	payload := []byte("hello world")
	hash := contentHash(payload)

	// Close the underlying volume file so the first frame write fails.
	vbs.mu.RLock()
	vf := vbs.activeVolume
	vbs.mu.RUnlock()
	if err := vf.file.Close(); err != nil {
		t.Fatalf("pre-close: %v", err)
	}

	if err := vbs.PutBlob(hash, bytes.NewReader(payload)); err == nil {
		t.Fatal("expected write failure on closed volume file")
	}
}

func TestVolumeBlobStore_GetDeleteInvalidAndMissing(t *testing.T) {
	vbs, cleanup := newTestVolumeStore(t)
	defer cleanup()

	absent := strings.Repeat("0", 64)

	if _, err := vbs.GetBlob("zzzz"); err == nil {
		t.Fatal("expected invalid-hash error")
	}
	if _, err := vbs.GetBlob(absent); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("expected ENOENT, got %v", err)
	}
	if err := vbs.DeleteBlob("zzzz"); err == nil {
		t.Fatal("expected invalid-hash error")
	}
	if err := vbs.DeleteBlob(absent); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("expected ENOENT, got %v", err)
	}
}

func TestVolumeBlobStore_DeleteSealedVolume(t *testing.T) {
	vbs, cleanup := newTestVolumeStore(t)
	defer cleanup()

	payload := []byte("sealed blob")
	hash := contentHash(payload)
	if err := vbs.PutBlob(hash, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}

	if err := vbs.sealCurrentVolume(); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := vbs.DeleteBlob(hash); err == nil {
		t.Fatal("expected delete on sealed volume to be refused")
	}
}

func TestVolumeBlobStore_CloseTwice(t *testing.T) {
	vbs, cleanup := newTestVolumeStore(t)
	defer cleanup()

	// Close must be idempotent enough not to panic on the second call.
	if err := vbs.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := vbs.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestVolumeBlobStore_GetBlobOnClosedVolumeFile(t *testing.T) {
	vbs, cleanup := newTestVolumeStore(t)
	defer cleanup()

	payload := []byte("will be unreadable")
	hash := contentHash(payload)
	if err := vbs.PutBlob(hash, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}

	vbs.mu.RLock()
	vf := vbs.activeVolume
	vbs.mu.RUnlock()
	_ = vf.file.Close()

	if _, err := vbs.GetBlob(hash); err == nil {
		t.Fatal("expected read failure on closed volume file")
	}
}

func TestVolumeBlobStore_MetadataReadErrors(t *testing.T) {
	vbs, cleanup := newTestVolumeStore(t)
	defer cleanup()

	if _, err := vbs.getVolumeMetadata(999); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("expected ENOENT, got %v", err)
	}
	if _, err := vbs.getNeedleMetadata(strings.Repeat("a", 64)); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("expected ENOENT, got %v", err)
	}
}

func TestVolumeBlobStore_LargeVolumeRejected(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewVolumeBlobStore(dir, 33<<30); err == nil {
		t.Fatal("expected volume size above max to be rejected")
	}
}

func TestNeedleCRC_MatchesReadNeedle(t *testing.T) {
	payload := []byte("crc contract")
	hashBytes := [32]byte{}
	for i := range hashBytes {
		hashBytes[i] = byte(i)
	}

	crc := needleCRC(hashBytes, payload)

	f, err := tempVolumeFile(t)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.appendNeedle(hashBytes, payload); err != nil {
		t.Fatalf("appendNeedle: %v", err)
	}
	needle, err := readNeedle(f.file, 0)
	if err != nil {
		t.Fatalf("readNeedle: %v", err)
	}
	if needle.Trailer.CRC32 != crc {
		t.Fatalf("CRC mismatch: wrote %x, read %x", crc, needle.Trailer.CRC32)
	}
	if !bytes.Equal(needle.Payload, payload) {
		t.Fatal("payload mismatch")
	}
}

// tempVolumeFile returns a VolumeFile backed by a scratch os.File.
func tempVolumeFile(t *testing.T) (*VolumeFile, error) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "needle-*")
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		name := f.Name()
		f.Close()
		os.Remove(name)
	})
	return &VolumeFile{file: f, path: f.Name(), volumeID: 1}, nil
}

var _ io.Reader = (*errReader)(nil)
