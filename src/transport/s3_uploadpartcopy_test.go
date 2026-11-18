package transport

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// copyStack boots an s3-tcp gateway over an in-memory-ish mock store that records
// what the assembler writes.
type copyStack struct {
	addr    string
	auth    string
	putData []byte
}

func newCopyStack(t *testing.T, source []byte) *copyStack {
	t.Helper()
	auth := "partcopy-token-1111111111111111111111111111111111111111111111" // notsecret
	cfg := common.Configuration{Global: common.ConfigurationGlobal{
		Protocol: "s3-tcp", AuthToken: auth, TLSInsecure: true,
	}}
	factory := NewProtocolFactory(cfg)
	l, err := factory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	stack := &copyStack{addr: l.Addr().String(), auth: auth}
	mock := &mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			return io.NopCloser(strings.NewReader(string(source))), common.FileMetadata{
				Name: name, Hash: "srchash", Size: int64(len(source)), ModTime: time.Now().UnixNano(),
			}, nil
		},
		putFunc: func(_ string, _ string, size int64, _ string, content io.Reader) error {
			data, rerr := io.ReadAll(content)
			if rerr != nil {
				return rerr
			}
			if int64(len(data)) != size {
				return fmt.Errorf("size mismatch: got %d want %d", len(data), size)
			}
			stack.putData = data
			return nil
		},
	}
	startServer(t, l, auth, mock, 5)
	return stack
}

// begin starts a multipart upload and returns its id.
func (c *copyStack) begin(t *testing.T) string {
	t.Helper()
	now := time.Now().UTC().Format("20060102T150405Z")
	req := fmt.Sprintf("POST /bucket/dest.bin?uploads HTTP/1.1\r\nHost: %s\r\nX-Amz-Date: %s\r\nAuthorization: Bearer %s\r\nContent-Length: 0\r\n\r\n",
		c.addr, now, c.auth)
	id := extractXMLTag(doS3Request(t, c.addr, req), "UploadId")
	if id == "" {
		t.Fatal("no UploadId")
	}
	return id
}

// copyPart issues an UploadPartCopy for the given part and returns the response.
func (c *copyStack) copyPart(t *testing.T, id string, part int, extraHeaders string) string {
	t.Helper()
	now := time.Now().UTC().Format("20060102T150405Z")
	req := fmt.Sprintf("PUT /bucket/dest.bin?uploadId=%s&partNumber=%d HTTP/1.1\r\n"+
		"Host: %s\r\nX-Amz-Date: %s\r\nAuthorization: Bearer %s\r\n"+
		"X-Amz-Copy-Source: /bucket/src.bin\r\n%sContent-Length: 0\r\n\r\n",
		id, part, c.addr, now, c.auth, extraHeaders)
	return doS3Request(t, c.addr, req)
}

// complete finishes the upload and returns the response.
func (c *copyStack) complete(t *testing.T, id string, parts ...int) string {
	t.Helper()
	var xml strings.Builder
	xml.WriteString("<CompleteMultipartUpload>")
	for _, p := range parts {
		fmt.Fprintf(&xml, "<Part><PartNumber>%d</PartNumber></Part>", p)
	}
	xml.WriteString("</CompleteMultipartUpload>")
	now := time.Now().UTC().Format("20060102T150405Z")
	body := xml.String()
	req := fmt.Sprintf("POST /bucket/dest.bin?uploadId=%s HTTP/1.1\r\n"+
		"Host: %s\r\nX-Amz-Date: %s\r\nAuthorization: Bearer %s\r\n"+
		"Content-Type: application/xml\r\nContent-Length: %d\r\n\r\n%s",
		id, c.addr, now, c.auth, len(body), body)
	return doS3Request(t, c.addr, req)
}

// TestUploadPartCopyWholeObject proves a copied part assembles into exactly the
// source bytes (issue #1365).
func TestUploadPartCopyWholeObject(t *testing.T) {
	t.Cleanup(func() { verifyNoLeaks(t) })

	source := []byte("server-side copied payload, not carried by the client")
	stack := newCopyStack(t, source)
	id := stack.begin(t)

	resp := stack.copyPart(t, id, 1, "")
	if !strings.Contains(resp, "200 OK") || !strings.Contains(resp, "<CopyPartResult") {
		t.Fatalf("expected CopyPartResult, got: %s", truncate(resp, 240))
	}
	if !strings.Contains(resp, "<ETag>") {
		t.Fatalf("CopyPartResult must carry an ETag: %s", truncate(resp, 240))
	}

	if done := stack.complete(t, id, 1); !strings.Contains(done, "200 OK") {
		t.Fatalf("complete failed: %s", truncate(done, 240))
	}
	if string(stack.putData) != string(source) {
		t.Fatalf("assembled %q, want %q", stack.putData, source)
	}
}

// TestUploadPartCopyRange proves x-amz-copy-source-range stores exactly the
// requested slice (inclusive on both ends, like S3).
func TestUploadPartCopyRange(t *testing.T) {
	t.Cleanup(func() { verifyNoLeaks(t) })

	source := []byte("0123456789abcdefghij")
	stack := newCopyStack(t, source)
	id := stack.begin(t)

	if resp := stack.copyPart(t, id, 1, "X-Amz-Copy-Source-Range: bytes=10-14\r\n"); !strings.Contains(resp, "200 OK") {
		t.Fatalf("ranged copy failed: %s", truncate(resp, 240))
	}
	if done := stack.complete(t, id, 1); !strings.Contains(done, "200 OK") {
		t.Fatalf("complete failed: %s", truncate(done, 240))
	}
	if string(stack.putData) != "abcde" {
		t.Fatalf("copied %q, want %q", stack.putData, "abcde")
	}
}

// TestUploadPartCopyRejectsBadRange proves an unsatisfiable or too-short range is
// refused instead of silently storing the wrong bytes.
func TestUploadPartCopyRejectsBadRange(t *testing.T) {
	t.Cleanup(func() { verifyNoLeaks(t) })

	for _, tc := range []struct{ name, header string }{
		{"beyond the object", "X-Amz-Copy-Source-Range: bytes=0-99\r\n"},
		{"inverted", "X-Amz-Copy-Source-Range: bytes=7-2\r\n"},
		{"too short", "X-Amz-Copy-Source-Range: bytes=0-2\r\n"},
		{"not a range", "X-Amz-Copy-Source-Range: 0-9\r\n"},
		{"open ended", "X-Amz-Copy-Source-Range: bytes=5-\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stack := newCopyStack(t, []byte("0123456789"))
			id := stack.begin(t)
			resp := stack.copyPart(t, id, 1, tc.header)
			if !strings.Contains(resp, "416") && !strings.Contains(resp, "InvalidRange") {
				t.Fatalf("expected InvalidRange, got: %s", truncate(resp, 240))
			}
		})
	}
}

// TestUploadPartCopyUnknownUpload proves a copy against a nonexistent session is
// refused with NoSuchUpload rather than creating one.
func TestUploadPartCopyUnknownUpload(t *testing.T) {
	t.Cleanup(func() { verifyNoLeaks(t) })

	stack := newCopyStack(t, []byte("payload"))
	resp := stack.copyPart(t, "no-such-upload", 1, "")
	if !strings.Contains(resp, "NoSuchUpload") {
		t.Fatalf("expected 404 NoSuchUpload, got: %s", truncate(resp, 240))
	}
}

// TestUploadPartCopyMissingSource proves a missing source object is reported as
// NoSuchKey.
func TestUploadPartCopyMissingSource(t *testing.T) {
	t.Cleanup(func() { verifyNoLeaks(t) })

	auth := "partcopy-miss-11111111111111111111111111111111111111111111111" // notsecret
	cfg := common.Configuration{Global: common.ConfigurationGlobal{
		Protocol: "s3-tcp", AuthToken: auth, TLSInsecure: true,
	}}
	factory := NewProtocolFactory(cfg)
	l, err := factory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()

	mock := &mockStore{
		getFunc: func(string) (io.ReadCloser, common.FileMetadata, error) {
			return nil, common.FileMetadata{}, fmt.Errorf("no such object: %w", syscall.ENOENT)
		},
	}
	startServer(t, l, auth, mock, 5)

	stack := &copyStack{addr: l.Addr().String(), auth: auth}
	id := stack.begin(t)
	resp := stack.copyPart(t, id, 1, "")
	if !strings.Contains(resp, "NoSuchKey") {
		t.Fatalf("expected 404 NoSuchKey, got: %s", truncate(resp, 240))
	}
}

// TestParseCopySourceRange covers the range grammar directly: the helper decides
// whether a copy stores the right bytes, so every rejection path is pinned.
func TestParseCopySourceRange(t *testing.T) {
	const size = 100
	ok := []struct {
		value string
		start int64
		len   int64
	}{
		{"", 0, size},
		{"bytes=0-4", 0, 5},
		{"bytes=95-99", 95, 5},
		{"bytes= 10-19 ", 10, 10},
	}
	for _, tc := range ok {
		start, length, err := parseCopySourceRange(tc.value, size)
		if err != nil {
			t.Fatalf("%q: unexpected error %v", tc.value, err)
		}
		if start != tc.start || length != tc.len {
			t.Fatalf("%q: got (%d,%d), want (%d,%d)", tc.value, start, length, tc.start, tc.len)
		}
	}

	bad := []string{
		"0-4",           // no unit
		"bytes=",        // empty spec
		"bytes=5",       // no separator
		"bytes=-5",      // missing start
		"bytes=5-",      // missing end
		"bytes=-5-9",    // negative start
		"bytes=9-5",     // inverted
		"bytes=0-100",   // end beyond the object
		"bytes=0-3",     // shorter than the 5-byte floor
		"bytes=abc-def", // not numbers
		"bytes=1-2-3",   // too many separators
	}
	for _, value := range bad {
		if _, _, err := parseCopySourceRange(value, size); err == nil {
			t.Fatalf("%q must be refused", value)
		}
	}
}

// TestSpoolReaderHashesAndCounts proves the copy spool returns the SHA-256 of the
// bytes it wrote and their length, and refuses an unusable destination.
func TestSpoolReaderHashesAndCounts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.part")
	etag, size, err := spoolReader(strings.NewReader("hello"), path)
	if err != nil {
		t.Fatalf("spoolReader: %v", err)
	}
	if size != 5 {
		t.Fatalf("size = %d, want 5", size)
	}
	// SHA-256("hello")
	if etag != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("etag = %q", etag)
	}
	data, rerr := os.ReadFile(path)
	if rerr != nil || string(data) != "hello" {
		t.Fatalf("spooled %q err=%v", data, rerr)
	}

	if _, _, serr := spoolReader(strings.NewReader("x"), filepath.Join(dir, "missing", "2.part")); serr == nil {
		t.Fatal("expected an error for an unusable destination")
	}
}
