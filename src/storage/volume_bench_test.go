package storage

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
)

func BenchmarkVolumePutBlob64KiB(b *testing.B) {
	dataDir := b.TempDir()
	vbs, err := NewVolumeBlobStore(dataDir, DefaultVolumeSize)
	if err != nil {
		b.Fatal(err)
	}
	defer vbs.Close()

	payload := make([]byte, 64*1024)
	if _, err := rand.Read(payload); err != nil {
		b.Fatal(err)
	}
	hash := contentHash(payload)

	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := vbs.PutBlob(hash, bytes.NewReader(payload)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVolumeGetBlob64KiB(b *testing.B) {
	dataDir := b.TempDir()
	vbs, err := NewVolumeBlobStore(dataDir, DefaultVolumeSize)
	if err != nil {
		b.Fatal(err)
	}
	defer vbs.Close()

	payload := make([]byte, 64*1024)
	if _, err := rand.Read(payload); err != nil {
		b.Fatal(err)
	}
	hash := contentHash(payload)
	if err := vbs.PutBlob(hash, bytes.NewReader(payload)); err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rc, err := vbs.GetBlob(hash)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, rc); err != nil {
			b.Fatal(err)
		}
		rc.Close()
	}
}

func BenchmarkAdaptivePutBlob64KiB(b *testing.B) {
	volumeDir, localDir := b.TempDir(), b.TempDir()
	volume, err := NewVolumeBlobStore(volumeDir, DefaultVolumeSize)
	if err != nil {
		b.Fatal(err)
	}
	defer volume.Close()
	local, err := NewLocalBlobStore(localDir)
	if err != nil {
		b.Fatal(err)
	}
	a := NewAdaptiveBlobStore(volume, local, 1<<20)

	payload := make([]byte, 64*1024)
	if _, err := rand.Read(payload); err != nil {
		b.Fatal(err)
	}
	hash := contentHash(payload)

	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.PutBlob(hash, bytes.NewReader(payload)); err != nil {
			b.Fatal(err)
		}
	}
}
