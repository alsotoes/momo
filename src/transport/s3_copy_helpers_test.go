package transport

import (
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// TestFetchCopySourceReportsMissingStore proves the guard answers an S3 error and
// returns ok=false instead of dereferencing a nil store.
func TestFetchCopySourceReportsMissingStore(t *testing.T) {
	comm, client := newPipeComm(t)
	go drainFor(client)

	if _, _, ok := comm.fetchCopySource("src"); ok {
		t.Fatal("a nil store must not report a copy source")
	}
}

// TestPartSpillDirCreatesOnceAndReuses proves the upload's spill directory is
// created lazily and shared by every part of the upload.
func TestPartSpillDirCreatesOnceAndReuses(t *testing.T) {
	comm, _ := newPipeComm(t)
	up := &multipartUpload{bucket: "b", key: "k", createdAt: time.Now()}

	first, err := comm.partSpillDir(up)
	if err != nil {
		t.Fatalf("partSpillDir: %v", err)
	}
	t.Cleanup(func() { removeSpill(first) })
	if first == "" {
		t.Fatal("partSpillDir returned an empty path")
	}

	second, err := comm.partSpillDir(up)
	if err != nil {
		t.Fatalf("partSpillDir (reuse): %v", err)
	}
	if second != first {
		t.Fatalf("spill dir changed: %q then %q", first, second)
	}
}

// TestAliasCopyPropagatesStorageFull proves an ENOSPC on the destination is
// reported, not swallowed.
func TestAliasCopyPropagatesStorageFull(t *testing.T) {
	comm, client := newPipeComm(t)
	go drainFor(client)
	comm.SetStore(&mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			return io.NopCloser(strings.NewReader("data")), common.FileMetadata{Name: name, Hash: "h", Size: 4}, nil
		},
		putFunc: func(string, string, int64, string, io.Reader) error {
			return syscall.ENOSPC
		},
	})

	if _, err := comm.aliasCopy("dst", "src", strings.NewReader("data")); err == nil {
		t.Fatal("expected an error when the store is full")
	}
}

// TestAliasCopyReportsMetadataFailure proves a source-metadata failure stops the
// copy instead of storing a blob it cannot describe.
func TestAliasCopyReportsMetadataFailure(t *testing.T) {
	comm, client := newPipeComm(t)
	go drainFor(client)
	comm.SetStore(&mockStore{
		getMetaFunc: func(string) (common.FileMetadata, error) {
			return common.FileMetadata{}, errors.New("metadata unavailable")
		},
	})

	if _, err := comm.aliasCopy("dst", "src", strings.NewReader("data")); err == nil {
		t.Fatal("expected an error when the source metadata cannot be read")
	}
}

// drainFor answers the S3 error bodies a helper writes, so a pipe-backed
// communicator never blocks.
func drainFor(client interface{ Read([]byte) (int, error) }) {
	buf := make([]byte, 1024)
	for {
		if _, err := client.Read(buf); err != nil {
			return
		}
	}
}
