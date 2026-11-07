package storage

import (
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"

	"github.com/alsotoes/momo/src/common"
)

func TestListBlobsAndDrop(t *testing.T) {
	s := newTestStore(t)
	payload := strings.Repeat("x", 32)
	hash := common.HashBytes([]byte(payload))
	if err := s.Put("a", hash, int64(len(payload)), "", strings.NewReader(payload)); err != nil {
		t.Fatalf("put: %v", err)
	}
	blobs, err := s.ListBlobs()
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if len(blobs) != 1 || blobs[0].Size != 32 || blobs[0].ModTime == 0 {
		t.Fatalf("unexpected blobs: %+v", blobs)
	}
	if blobs[0].Hash != hash {
		t.Fatalf("unexpected hash %s", blobs[0].Hash)
	}

	rc, err := s.GetBlobByHash(hash)
	if err != nil {
		t.Fatalf("GetBlobByHash: %v", err)
	}
	data, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(data) != payload {
		t.Fatalf("blob bytes mismatch")
	}

	if err := s.DropBlob(hash); err != nil {
		t.Fatalf("DropBlob: %v", err)
	}
	blobs, err = s.ListBlobs()
	if err != nil {
		t.Fatalf("ListBlobs after drop: %v", err)
	}
	if len(blobs) != 0 {
		t.Fatalf("blob not dropped: %+v", blobs)
	}
	// Metadata survives: the namespace still points at the hash.
	if _, err := s.GetMeta("a"); err != nil {
		t.Fatalf("metadata must survive a shed: %v", err)
	}
}

func TestGetBlobByHashRejectsTraversal(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.GetBlobByHash("../etc/passwd"); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("want EINVAL, got %v", err)
	}
}

func TestBlobSize(t *testing.T) {
	s := newTestStore(t)
	payload := []byte(strings.Repeat("z", 40))
	hash := common.HashBytes(payload)
	if err := s.Put("size", hash, int64(len(payload)), "", strings.NewReader(string(payload))); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := s.BlobSize(hash)
	if err != nil {
		t.Fatalf("BlobSize: %v", err)
	}
	if got != int64(len(payload)) {
		t.Fatalf("size = %d, want %d", got, len(payload))
	}
	if _, err := s.BlobSize("missing"); err == nil {
		t.Fatal("missing blob must error")
	}
	if _, err := s.BlobSize("../etc/passwd"); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("want EINVAL, got %v", err)
	}
}

func TestPutRestoresShedBlobBytes(t *testing.T) {
	s := newTestStore(t)
	payload := []byte(strings.Repeat("r", 24))
	hash := common.HashBytes(payload)
	if err := s.Put("keep", hash, int64(len(payload)), "", strings.NewReader(string(payload))); err != nil {
		t.Fatalf("put: %v", err)
	}
	// Shedding removes bytes but keeps metadata (rebalance design).
	if err := s.DropBlob(hash); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := s.GetBlobByHash(hash); err == nil {
		t.Fatal("bytes should be gone after a shed")
	}
	// A push re-storing the same hash must write the bytes again, not just ack.
	if err := s.Put("keep", hash, int64(len(payload)), "", strings.NewReader(string(payload))); err != nil {
		t.Fatalf("re-put: %v", err)
	}
	rc, err := s.GetBlobByHash(hash)
	if err != nil {
		t.Fatalf("blob bytes were not restored: %v", err)
	}
	data, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(data) != string(payload) {
		t.Fatalf("restored bytes mismatch: %q", data)
	}
	if blobs, _ := s.ListBlobs(); len(blobs) != 1 {
		t.Fatalf("expected one blob, got %d", len(blobs))
	}
}
