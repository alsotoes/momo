package storage

import (
	"bytes"
	"testing"
)

func seedBlob(t *testing.T, store BlobStore, payload []byte) string {
	t.Helper()
	hash := contentHash(payload)
	if err := store.PutBlob(hash, bytes.NewReader(payload)); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	return hash
}

func assertPresence(t *testing.T, store blobPresence, hash string) {
	t.Helper()
	ok, err := store.BlobExists(hash)
	if err != nil {
		t.Fatalf("BlobExists: %v", err)
	}
	if !ok {
		t.Fatal("existing blob not reported present")
	}
	if err := store.(BlobStore).DeleteBlob(hash); err != nil {
		t.Fatalf("DeleteBlob: %v", err)
	}
	ok, err = store.BlobExists(hash)
	if err != nil {
		t.Fatalf("BlobExists after delete: %v", err)
	}
	if ok {
		t.Fatal("deleted blob still reported present")
	}
}

func TestLocalBlobStore_BlobExists(t *testing.T) {
	store, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBlobStore: %v", err)
	}
	hash := seedBlob(t, store, []byte("local-presence"))
	assertPresence(t, store, hash)
	if _, err := store.BlobExists("../etc/passwd"); err == nil {
		t.Fatal("traversal hash must be rejected")
	}
}

func TestVolumeBlobStore_BlobExists(t *testing.T) {
	store, err := NewVolumeBlobStore(t.TempDir(), DefaultVolumeSize)
	if err != nil {
		t.Fatalf("NewVolumeBlobStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	hash := seedBlob(t, store, []byte("volume-presence"))
	assertPresence(t, store, hash)
	if _, err := store.BlobExists("not-a-hash"); err == nil {
		t.Fatal("invalid hash must be rejected")
	}
}

func TestAdaptiveBlobStore_BlobExists(t *testing.T) {
	a, _, cleanup := newTestAdaptiveStore(t, 1<<10)
	defer cleanup()

	small := make([]byte, 128)
	smallHash := seedBlob(t, a, small) // routed to the volume store
	ok, err := a.BlobExists(smallHash)
	if err != nil || !ok {
		t.Fatalf("volume-routed blob: ok=%v err=%v", ok, err)
	}

	large := bytes.Repeat([]byte("L"), 4096)
	largeHash := seedBlob(t, a, large) // routed to the local fallback
	ok, err = a.BlobExists(largeHash)
	if err != nil || !ok {
		t.Fatalf("fallback-routed blob: ok=%v err=%v", ok, err)
	}

	if err := a.DeleteBlob(largeHash); err != nil {
		t.Fatalf("DeleteBlob: %v", err)
	}
	ok, err = a.BlobExists(largeHash)
	if err != nil || ok {
		t.Fatalf("deleted fallback blob: ok=%v err=%v", ok, err)
	}
}

func TestAdaptiveBlobStore_BlobExistsWithoutVolume(t *testing.T) {
	local, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBlobStore: %v", err)
	}
	a := NewAdaptiveBlobStore(nil, local, 1<<20)
	// Above the 1 MiB boundary: the blob routes to the fallback backend.
	payload := bytes.Repeat([]byte("F"), (1<<20)+1024)
	hash := seedBlob(t, a, payload)
	if ok, err := a.BlobExists(hash); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestEncryptedBlobStore_BlobExistsForwards(t *testing.T) {
	inner, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBlobStore: %v", err)
	}
	enc, err := NewEncryptedBlobStore(inner, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatalf("NewEncryptedBlobStore: %v", err)
	}
	hash := seedBlob(t, enc, []byte("encrypted-presence"))
	assertPresence(t, enc, hash)
}

// opaqueStore is a BlobStore without the presence seam.
type opaqueStore struct{ BlobStore }

func TestEncryptedBlobStore_BlobExistsWithoutInnerSupport(t *testing.T) {
	enc, err := NewEncryptedBlobStore(opaqueStore{}, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatalf("NewEncryptedBlobStore: %v", err)
	}
	ok, err := enc.BlobExists("any-hash")
	if err != nil || !ok {
		t.Fatalf("an opaque inner backend must be assumed present: ok=%v err=%v", ok, err)
	}
}
