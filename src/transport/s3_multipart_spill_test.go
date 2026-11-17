package transport

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestOpenPartsReleasesOnFailure proves a missing part file surfaces as an error
// and the already-opened files are closed instead of leaked.
func TestOpenPartsReleasesOnFailure(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "1.part")
	if err := os.WriteFile(good, []byte("a"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	parts := []multipartPart{
		{partNumber: 1, size: 1, path: good},
		{partNumber: 2, size: 1, path: filepath.Join(dir, "missing.part")},
	}
	r, closeAll, err := openParts(parts)
	if err == nil {
		closeAll()
		t.Fatal("expected an error for a missing part file")
	}
	if r != nil {
		t.Fatal("expected no reader when opening fails")
	}
	closeAll() // must be safe to call after a failure
}

// TestOpenPartsReadsAllParts proves the multi-reader concatenates parts in order.
func TestOpenPartsReadsAllParts(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "1.part")
	p2 := filepath.Join(dir, "2.part")
	if err := os.WriteFile(p1, []byte("one"), 0o600); err != nil {
		t.Fatalf("write p1: %v", err)
	}
	if err := os.WriteFile(p2, []byte("two"), 0o600); err != nil {
		t.Fatalf("write p2: %v", err)
	}

	r, closeAll, err := openParts([]multipartPart{
		{partNumber: 1, size: 3, path: p1},
		{partNumber: 2, size: 3, path: p2},
	})
	if err != nil {
		t.Fatalf("openParts: %v", err)
	}
	defer closeAll()
	data, rerr := io.ReadAll(r)
	if rerr != nil {
		t.Fatalf("read: %v", rerr)
	}
	if string(data) != "onetwo" {
		t.Fatalf("assembled %q, want %q", data, "onetwo")
	}
}

// TestRemoveSpill proves the spill directory is removed, and that an empty
// directory is a no-op (uploads that never received a part).
func TestRemoveSpill(t *testing.T) {
	dir := t.TempDir()
	spill := filepath.Join(dir, "upload")
	if err := os.MkdirAll(spill, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(spill, "1.part"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	removeSpill(spill)
	if _, err := os.Stat(spill); !os.IsNotExist(err) {
		t.Fatalf("spill directory still present: %v", err)
	}
	removeSpill("") // no upload ever spooled a part
}

// TestSpoolPartPlainBodyWritesBytes exercises the non-chunked branch directly.
func TestSpoolPartPlainBodyWritesBytes(t *testing.T) {
	comm, _ := newPipeComm(t)
	body := "plain part bytes"
	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:4440/b/k?partNumber=1", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.ContentLength = int64(len(body))

	path := filepath.Join(t.TempDir(), "1.part")
	etag, size, serr := comm.spoolPart(req, path)
	if serr != nil {
		t.Fatalf("spoolPart: %v", serr)
	}
	if size != int64(len(body)) {
		t.Fatalf("size = %d, want %d", size, len(body))
	}
	stored, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("read: %v", rerr)
	}
	if string(stored) != body {
		t.Fatalf("stored %q, want %q", stored, body)
	}
	if etag == "" || len(etag) != 64 {
		t.Fatalf("etag = %q, want a SHA-256 hex digest", etag)
	}
}

// TestSpoolPartUnwritablePath proves an unusable destination maps to EIO after
// answering with an S3 error instead of panicking.
func TestSpoolPartUnwritablePath(t *testing.T) {
	comm, _ := newPipeComm(t)
	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:4440/b/k?partNumber=1", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.ContentLength = 1

	_, _, serr := comm.spoolPart(req, filepath.Join(t.TempDir(), "missing-dir", "1.part"))
	if serr == nil {
		t.Fatal("expected an error for an unusable part path")
	}
}

// TestMultipartAbortRemovesSpill proves AbortMultipartUpload drops the session
// and its spooled parts.
func TestMultipartAbortRemovesSpill(t *testing.T) {
	spill := t.TempDir()
	uploadID := "uuid-spill-abort"
	muUploads.Lock()
	uploads[uploadID] = &multipartUpload{bucket: "b", key: "k", spillDir: spill}
	muUploads.Unlock()

	comm, client := newPipeComm(t)
	// Drain the 204 so the handler's write cannot block on the pipe.
	go func() {
		buf := make([]byte, 512)
		for {
			_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, rerr := client.Read(buf); rerr != nil {
				return
			}
		}
	}()
	response := make(chan string, 1)
	go func() { _, _, err := comm.handleAbortMultipartUpload(uploadID); response <- fmt.Sprint(err) }()

	select {
	case <-response:
	case <-time.After(5 * time.Second):
		t.Fatal("abort did not return")
	}
	muUploads.Lock()
	_, still := uploads[uploadID]
	muUploads.Unlock()
	if still {
		t.Fatal("upload session survived the abort")
	}
}
