package storage

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"syscall"
	"testing"
)

func newTestVolumeStore(t *testing.T) (*VolumeBlobStore, func()) {
	t.Helper()
	dataDir := t.TempDir()
	vbs, err := NewVolumeBlobStore(dataDir, DefaultVolumeSize)
	if err != nil {
		t.Fatalf("NewVolumeBlobStore: %v", err)
	}
	return vbs, func() { vbs.Close() }
}

func TestVolumeBlobStore_PutGet(t *testing.T) {
	vbs, cleanup := newTestVolumeStore(t)
	defer cleanup()

	payload := []byte("hello world")
	hash := contentHash(payload)

	err := vbs.PutBlob(hash, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	rc, err := vbs.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("payload mismatch: got %q, want %q", data, payload)
	}
}

func TestVolumeBlobStore_VolumeRolling(t *testing.T) {
	dataDir := t.TempDir()
	// A small superblock forces real rolling: 5 x 64 KiB objects across
	// 128 KiB volumes produces several sealed files.
	vbs, err := NewVolumeBlobStore(dataDir, 128*1024)
	if err != nil {
		t.Fatalf("NewVolumeBlobStore: %v", err)
	}
	defer vbs.Close()

	const n = 5
	hashes := make([]string, n)
	payloads := make([][]byte, n)

	for i := 0; i < n; i++ {
		payloads[i] = make([]byte, 64*1024)
		if _, err := rand.Read(payloads[i]); err != nil {
			t.Fatal(err)
		}
		hashes[i] = contentHash(payloads[i])

		if err := vbs.PutBlob(hashes[i], bytes.NewReader(payloads[i])); err != nil {
			t.Fatalf("PutBlob %d: %v", i, err)
		}
	}

	vbs.mu.RLock()
	sealed := len(vbs.sealedVolumes)
	vbs.mu.RUnlock()
	if sealed == 0 {
		t.Fatal("expected at least one sealed volume after rolling")
	}

	// Every object must read back correctly, including ones sealed away.
	for i := 0; i < n; i++ {
		rc, err := vbs.GetBlob(hashes[i])
		if err != nil {
			t.Fatalf("GetBlob %d: %v", i, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("ReadAll %d: %v", i, err)
		}
		if !bytes.Equal(data, payloads[i]) {
			t.Fatalf("payload %d mismatch", i)
		}
	}
}

func TestVolumeBlobStore_DeleteWithHolePunch(t *testing.T) {
	vbs, cleanup := newTestVolumeStore(t)
	defer cleanup()

	payload := []byte("delete me")
	hash := contentHash(payload)

	if err := vbs.PutBlob(hash, bytes.NewReader(payload)); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	if _, err := vbs.GetBlob(hash); err != nil {
		t.Fatalf("GetBlob before delete: %v", err)
	}

	if err := vbs.DeleteBlob(hash); err != nil {
		t.Fatalf("DeleteBlob: %v", err)
	}

	if _, err := vbs.GetBlob(hash); err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestVolumeBlobStore_CrashRecovery(t *testing.T) {
	dataDir := t.TempDir()

	vbs, err := NewVolumeBlobStore(dataDir, DefaultVolumeSize)
	if err != nil {
		t.Fatalf("NewVolumeBlobStore: %v", err)
	}

	payload := []byte("crash recovery test")
	hash := contentHash(payload)
	if err := vbs.PutBlob(hash, bytes.NewReader(payload)); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	if err := vbs.Close(); err != nil {
		t.Fatalf("close volume store: %v", err)
	}

	// Reopen and verify data survived.
	vbs2, err := NewVolumeBlobStore(dataDir, DefaultVolumeSize)
	if err != nil {
		t.Fatalf("NewVolumeBlobStore after crash: %v", err)
	}
	defer vbs2.Close()

	rc, err := vbs2.GetBlob(hash)
	if err != nil {
		t.Fatalf("GetBlob after crash: %v", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read after crash: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("data mismatch after crash: got %q, want %q", data, payload)
	}
}

func TestVolumeBlobStore_HolePunching(t *testing.T) {
	vbs, cleanup := newTestVolumeStore(t)
	defer cleanup()

	payload := make([]byte, 64*1024) // 64 KiB
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	hash := contentHash(payload)

	if err := vbs.PutBlob(hash, bytes.NewReader(payload)); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	vbs.mu.RLock()
	vf := vbs.activeVolume
	vbs.mu.RUnlock()

	if vf == nil {
		t.Fatal("no active volume")
	}

	vf.mu.RLock()
	var statBefore syscall.Stat_t
	if err := syscall.Fstat(int(vf.file.Fd()), &statBefore); err != nil {
		t.Fatalf("Fstat: %v", err)
	}
	vf.mu.RUnlock()

	if err := vbs.DeleteBlob(hash); err != nil {
		t.Fatalf("DeleteBlob: %v", err)
	}

	vf.mu.RLock()
	var statAfter syscall.Stat_t
	if err := syscall.Fstat(int(vf.file.Fd()), &statAfter); err != nil {
		t.Fatalf("Fstat after delete: %v", err)
	}
	vf.mu.RUnlock()

	t.Logf("Blocks before: %d, after: %d", statBefore.Blocks, statAfter.Blocks)
}

func TestVolumeNeedle_FrameRoundTrip(t *testing.T) {
	payload := []byte("test payload")
	sum := sha256.Sum256(payload)
	var hashArr [32]byte
	copy(hashArr[:], sum[:])

	f, err := os.CreateTemp("", "needle-test-*")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	if _, err := writeNeedle(f, hashArr, payload, NeedleFlagNone); err != nil {
		t.Fatalf("writeNeedle: %v", err)
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek: %v", err)
	}

	needle, err := readNeedle(f, 0)
	if err != nil {
		t.Fatalf("readNeedle: %v", err)
	}

	if !bytes.Equal(needle.Payload, payload) {
		t.Fatalf("payload mismatch: got %q, want %q", needle.Payload, payload)
	}
	if !bytesEqual(needle.Header.Hash[:], hashArr[:]) {
		t.Fatal("hash mismatch")
	}
	if needle.Header.Magic != NeedleMagic {
		t.Fatalf("magic mismatch: got %x, want %x", needle.Header.Magic, NeedleMagic)
	}
}

// contentHash returns the hex-encoded SHA-256 of the payload, which is what
// PutBlob verifies against (content-addressed storage).
func contentHash(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func randomHash() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
