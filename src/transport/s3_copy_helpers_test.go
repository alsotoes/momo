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

// metaStore is a mockStore that also persists S3 metadata, like the real store.
type metaStore struct {
	mockStore
	saved map[string]map[string]string
	meta  map[string]map[string]string
	err   error
}

func (m *metaStore) GetS3Meta(key string) map[string]string { return m.meta[key] }

func (m *metaStore) PutS3Meta(key string, headers map[string]string) error {
	if m.saved == nil {
		m.saved = make(map[string]map[string]string)
	}
	m.saved[key] = headers
	return m.err
}

// TestFetchCopySourceBranches proves each outcome of opening a copy source: a
// usable reader, a wrapped not-found, and an unrelated failure.
func TestFetchCopySourceBranches(t *testing.T) {
	comm, client := newPipeComm(t)
	go drainFor(client)

	comm.SetStore(&mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			return io.NopCloser(strings.NewReader("ok")), common.FileMetadata{Name: name, Size: 2}, nil
		},
	})
	rc, size, ok := comm.fetchCopySource("src")
	if !ok || size != 2 {
		t.Fatalf("expected a readable source, got ok=%v size=%d", ok, size)
	}
	_ = rc.Close()

	comm.SetStore(&mockStore{
		getFunc: func(string) (io.ReadCloser, common.FileMetadata, error) {
			return nil, common.FileMetadata{}, wrappedNotFound{}
		},
	})
	if _, _, ok := comm.fetchCopySource("src"); ok {
		t.Fatal("a wrapped not-found must not report a source")
	}

	comm.SetStore(&mockStore{
		getFunc: func(string) (io.ReadCloser, common.FileMetadata, error) {
			return nil, common.FileMetadata{}, errors.New("backend offline")
		},
	})
	if _, _, ok := comm.fetchCopySource("src"); ok {
		t.Fatal("a backend failure must not report a source")
	}
}

// wrappedNotFound is a not-found error that only unwraps to ENOENT.
type wrappedNotFound struct{}

func (wrappedNotFound) Error() string { return "no such object" }
func (wrappedNotFound) Unwrap() error { return syscall.ENOENT }

// TestAliasCopyCarriesSourceMetadata proves the destination alias receives the
// source's S3 metadata, and that a failure to write it does not fail the copy.
func TestAliasCopyCarriesSourceMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"metadata saved", nil},
		{"metadata write fails", errors.New("bolt unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			comm, client := newPipeComm(t)
			go drainFor(client)
			store := &metaStore{err: tc.err}
			store.getFunc = func(name string) (io.ReadCloser, common.FileMetadata, error) {
				return io.NopCloser(strings.NewReader("data")), common.FileMetadata{Name: name, Hash: "h", Size: 4}, nil
			}
			store.putFunc = func(string, string, int64, string, io.Reader) error { return nil }
			comm.SetStore(store)
			store.meta = map[string]map[string]string{
				"src": {"Content-Type": "text/plain"},
			}

			if _, err := comm.aliasCopy("dst", "src", strings.NewReader("data")); err != nil {
				t.Fatalf("aliasCopy: %v", err)
			}
			if store.saved["dst"] == nil {
				t.Fatalf("metadata was not carried to the alias: %#v", store.saved)
			}
		})
	}
}
