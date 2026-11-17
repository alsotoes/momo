package transport

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// parsedMetaFor drives HandshakeServer for a raw PUT path and returns the
// metadata it parsed for the object.
func parsedMetaFor(t *testing.T, path string) common.FileMetadata {
	t.Helper()
	const token = "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, dialErr := net.Dial("tcp", ln.Addr().String())
		if dialErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = conn.Write([]byte("PUT " + path + " HTTP/1.1\r\n" +
			"Host: 127.0.0.1:4440\r\n" +
			"Authorization: Bearer " + token + "\r\n" +
			"X-Amz-Content-Sha256: abc123\r\n" +
			"Content-Length: 3\r\n\r\nabc"))
		buf := make([]byte, 128)
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		_, _ = conn.Read(buf)
	}()

	server, accErr := ln.Accept()
	if accErr != nil {
		t.Fatalf("accept: %v", accErr)
	}
	defer func() { _ = server.Close() }()

	comm := NewS3Communicator(server)
	if _, _, hsErr := comm.HandshakeServer([]byte(common.PadString(token, common.AuthTokenLength))); hsErr != nil {
		t.Fatalf("handshake: %v", hsErr)
	}
	return comm.meta
}

// TestS3PathKeyIsBucketRelative pins the object name for a path-style PUT: the
// stored name is the bucket-relative key, which is what GET/HEAD/DELETE resolve.
// Storing the full path made hierarchical objects unreadable (issue #1348).
func TestS3PathKeyIsBucketRelative(t *testing.T) {
	meta := parsedMetaFor(t, "/testb/dir/key.txt")
	if meta.Name != "dir/key.txt" {
		t.Fatalf("stored name = %q, want %q (bucket-relative)", meta.Name, "dir/key.txt")
	}
}

// TestS3TopLevelPathStaysFlat pins legacy flat mode: a path with no bucket
// segment is a top-level object stored under its own name.
func TestS3TopLevelPathStaysFlat(t *testing.T) {
	meta := parsedMetaFor(t, "/top-level.txt")
	if meta.Name != "top-level.txt" {
		t.Fatalf("stored name = %q, want %q", meta.Name, "top-level.txt")
	}
}

// TestS3NestedPathRoundTrips proves the write name and the read key agree for a
// deep path, which is the invariant a real S3 client depends on.
func TestS3NestedPathRoundTrips(t *testing.T) {
	for _, path := range []string{"/b/k", "/b/a/b/c.txt", "/bucket/.hidden", "/b/trailing/"} {
		meta := parsedMetaFor(t, path)
		req, rErr := http.NewRequest(http.MethodPut, "http://127.0.0.1:4440"+path, nil)
		if rErr != nil {
			t.Fatalf("build request for %q: %v", path, rErr)
		}
		_, key := extractS3BucketAndKey(req)
		want := key
		if want == "" {
			want = strings.TrimPrefix(path, "/")
		}
		if meta.Name != want {
			t.Fatalf("path %q: stored %q, read resolves %q", path, meta.Name, want)
		}
	}
}
