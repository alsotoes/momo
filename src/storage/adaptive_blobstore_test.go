package storage

import (
	"bytes"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestAdaptiveStore(t *testing.T, threshold int64) (*AdaptiveBlobStore, string, func()) {
	t.Helper()
	volumeDir := t.TempDir()
	localDir := t.TempDir()

	volume, err := NewVolumeBlobStore(volumeDir, DefaultVolumeSize)
	if err != nil {
		t.Fatalf("NewVolumeBlobStore: %v", err)
	}
	local, err := NewLocalBlobStore(localDir)
	if err != nil {
		t.Fatalf("NewLocalBlobStore: %v", err)
	}
	return NewAdaptiveBlobStore(volume, local, threshold), volumeDir, func() { volume.Close() }
}

func TestAdaptiveBlobStore_RoutesSmallToVolume(t *testing.T) {
	a, volumeDir, cleanup := newTestAdaptiveStore(t, 1<<20)
	defer cleanup()

	payload := make([]byte, 64*1024) // 64 KiB -> volume
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	hash := contentHash(payload)

	if err := a.PutBlob(hash, bytes.NewReader(payload)); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	// Must be readable from the volume store directly.
	rc, err := a.volume.GetBlob(hash)
	if err != nil {
		t.Fatalf("expected blob in volume store: %v", err)
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("volume read mismatch: err=%v", err)
	}

	// And the volume dir must actually hold a superblock file.
	entries, err := os.ReadDir(volumeDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if filepath.Ext(e.Name()) == VolumeFileExt {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a volume file in the volume dir")
	}
}

func TestAdaptiveBlobStore_RoutesLargeToFallback(t *testing.T) {
	a, _, cleanup := newTestAdaptiveStore(t, 1<<20)
	defer cleanup()

	payload := make([]byte, 2<<20) // 2 MiB -> fallback
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	hash := contentHash(payload)

	if err := a.PutBlob(hash, bytes.NewReader(payload)); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	// Must NOT be in the volume store.
	if _, err := a.volume.GetBlob(hash); err == nil {
		t.Fatal("large blob unexpectedly landed in the volume store")
	}
	// Must be readable through the seam.
	rc, err := a.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("fallback read mismatch: err=%v", err)
	}
}

func TestAdaptiveBlobStore_GetAndDelete(t *testing.T) {
	a, _, cleanup := newTestAdaptiveStore(t, 1<<20)
	defer cleanup()

	small := []byte("small object")
	big := make([]byte, 3<<20)
	if _, err := rand.Read(big); err != nil {
		t.Fatal(err)
	}
	smallHash, bigHash := contentHash(small), contentHash(big)

	if err := a.PutBlob(smallHash, bytes.NewReader(small)); err != nil {
		t.Fatal(err)
	}
	if err := a.PutBlob(bigHash, bytes.NewReader(big)); err != nil {
		t.Fatal(err)
	}

	// Both readable through the seam.
	for h, want := range map[string][]byte{smallHash: small, bigHash: big} {
		rc, err := a.GetBlob(h)
		if err != nil {
			t.Fatalf("GetBlob(%s): %v", h[:12], err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil || !bytes.Equal(data, want) {
			t.Fatalf("read mismatch for %s: err=%v", h[:12], err)
		}
	}

	// Deletes are no-ops on missing blobs and succeed on present ones.
	if err := a.DeleteBlob(smallHash); err != nil {
		t.Fatalf("DeleteBlob small: %v", err)
	}
	absent := strings.Repeat("0", 64) // valid hash, never written
	if err := a.DeleteBlob(absent); err != nil {
		t.Fatalf("DeleteBlob missing should be a no-op: %v", err)
	}
	if err := a.DeleteBlob("deadbeef"); err == nil {
		t.Fatal("expected error for invalid hash")
	}
}

func TestAdaptiveBlobStore_Validation(t *testing.T) {
	a, _, cleanup := newTestAdaptiveStore(t, 1<<20)
	defer cleanup()

	if err := a.PutBlob("../evil", bytes.NewReader([]byte("x"))); err == nil {
		t.Fatal("expected path-traversal rejection")
	}
	if _, err := a.GetBlob("../evil"); err == nil {
		t.Fatal("expected path-traversal rejection")
	}
}

func TestAdaptiveBlobStore_NoBackends(t *testing.T) {
	a := NewAdaptiveBlobStore(nil, nil, 1<<20)
	if err := a.PutBlob(contentHash([]byte("x")), bytes.NewReader([]byte("x"))); err == nil {
		t.Fatal("expected error when no backends are configured")
	}
}
