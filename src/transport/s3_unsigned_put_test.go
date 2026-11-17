package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

const unsignedPutToken = "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

// unsignedPutRequest builds a plain S3 PUT: no content hash, no momo framing.
func unsignedPutRequest(body string, declared int, headers ...string) string {
	req := "PUT /bucket/key HTTP/1.1\r\nHost: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + unsignedPutToken + "\r\n"
	for _, h := range headers {
		req += h + "\r\n"
	}
	return req + "Content-Length: " + strconv.Itoa(declared) + "\r\n\r\n" + body
}

// driveUnsignedPut runs HandshakeServer over a real socket against a scripted
// client and returns the parsed metadata plus everything the server wrote.
// closeAfter controls whether the client half-closes once the request is sent.
func driveUnsignedPut(t *testing.T, request string, closeAfter bool) (common.FileMetadata, string, error) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	responses := make(chan string, 1)
	go func() { responses <- scriptedPutClient(ln.Addr().String(), request, closeAfter) }()

	server, accErr := ln.Accept()
	if accErr != nil {
		t.Fatalf("accept: %v", accErr)
	}
	defer func() { _ = server.Close() }()

	comm := NewS3Communicator(server)
	_, _, hsErr := comm.HandshakeServer([]byte(common.PadString(unsignedPutToken, common.AuthTokenLength)))

	select {
	case out := <-responses:
		return comm.meta, out, hsErr
	case <-time.After(2 * time.Second):
		return comm.meta, "", hsErr
	}
}

// scriptedPutClient sends one raw request and collects everything the server
// writes back until the connection goes quiet.
func scriptedPutClient(addr, request string, closeAfter bool) string {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return ""
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.Write([]byte(request)); err != nil {
		return ""
	}
	if tcp, ok := conn.(*net.TCPConn); ok && closeAfter {
		_ = tcp.CloseWrite()
	}

	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		n, readErr := conn.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if readErr != nil {
			return sb.String()
		}
	}
}

// TestS3UnsignedPutDerivesHash proves an external PUT with no
// X-Amz-Content-Sha256 is buffered, hashed and replayed instead of refused
// (issue #1346).
func TestS3UnsignedPutDerivesHash(t *testing.T) {
	body := "unsigned payload for the transport-level test"
	meta, _, err := driveUnsignedPut(t, unsignedPutRequest(body, len(body)), false)
	if err != nil {
		t.Fatalf("handshake refused an unsigned PUT: %v", err)
	}

	sum := sha256.Sum256([]byte(body))
	if want := hex.EncodeToString(sum[:]); meta.Hash != want {
		t.Fatalf("derived hash = %q, want %q", meta.Hash, want)
	}
	if meta.Size != int64(len(body)) {
		t.Fatalf("derived size = %d, want %d", meta.Size, len(body))
	}
}

// TestS3UnsignedPutHonoursExpectContinue proves the gateway answers
// 100-continue before pulling the body.
func TestS3UnsignedPutHonoursExpectContinue(t *testing.T) {
	body := "expect-continue body"
	_, out, err := driveUnsignedPut(t, unsignedPutRequest(body, len(body), "Expect: 100-continue"), false)
	if err != nil {
		t.Fatalf("handshake refused the upload: %v", err)
	}
	if !strings.Contains(out, "100 Continue") {
		t.Fatalf("expected a 100 Continue interim response, got %q", out)
	}
}

// TestS3UnsignedPutRejectsOversizedLength proves an over-limit declared length
// is refused with an S3 413 instead of a silent close.
func TestS3UnsignedPutRejectsOversizedLength(t *testing.T) {
	_, out, err := driveUnsignedPut(t, unsignedPutRequest("", int(common.MaxFileSize)+1), false)
	if err == nil {
		t.Fatal("expected the handshake to refuse an oversized upload")
	}
	if !strings.Contains(out, "413") || !strings.Contains(out, "EntityTooLarge") {
		t.Fatalf("expected an S3 413 EntityTooLarge response, got %q", out)
	}
}

// TestS3UnsignedPutRejectsTruncatedBody proves a body cut short by the client is
// answered with an S3 400 rather than a truncated object.
func TestS3UnsignedPutRejectsTruncatedBody(t *testing.T) {
	_, out, err := driveUnsignedPut(t, unsignedPutRequest("short", 40), true)
	if err == nil {
		t.Fatal("expected the handshake to refuse a truncated body")
	}
	if !strings.Contains(out, "400") {
		t.Fatalf("expected an S3 400 response, got %q", out)
	}
}

// TestS3ExternalClientGetsNoInterimFraming proves the interim momo responses and
// the ACK frame are suppressed for external clients, and that the final ACK is a
// standard S3 200 carrying the content ETag (issue #1346).
func TestS3ExternalClientGetsNoInterimFraming(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	comm := NewS3Communicator(server)
	comm.isExternalClient = true
	comm.meta.Hash = strings.Repeat("ab", sha256.Size)

	reads := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		_ = client.SetReadDeadline(time.Now().Add(time.Second))
		n, _ := client.Read(buf)
		reads <- string(buf[:n])
	}()

	if err := comm.SendReplicationMode(common.ReplicationChain); err != nil {
		t.Fatalf("SendReplicationMode: %v", err)
	}
	if err := comm.SendMetadataStatus(MetadataStatusSendPayload); err != nil {
		t.Fatalf("SendMetadataStatus: %v", err)
	}
	if err := comm.SendACK(0); err != nil {
		t.Fatalf("SendACK: %v", err)
	}

	got := <-reads
	if strings.Contains(got, "X-Momo-Replication-Mode") || strings.Contains(got, "X-Momo-Metadata-Status") {
		t.Fatalf("external client received interim momo framing: %q", got)
	}
	if !strings.HasPrefix(got, "HTTP/1.1 200 OK") || !strings.Contains(got, "ETag: \""+comm.meta.Hash+"\"") {
		t.Fatalf("expected an S3 200 with the content ETag, got %q", got)
	}
}

// erroringBody fails every read, standing in for a client that dies mid-upload.
type erroringBody struct{}

func (erroringBody) Read([]byte) (int, error) { return 0, syscall.EIO }
func (erroringBody) Close() error             { return nil }

// TestBufferUnsignedPayloadErrorPaths pins the refusal branches: an unreadable
// body, a short body, and an unusable spill directory all produce an S3 error
// instead of a truncated object or a silent close (issue #1346).
func TestBufferUnsignedPayloadErrorPaths(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			_ = client.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			if _, err := client.Read(buf); err != nil {
				return
			}
		}
	}()

	newComm := func() *S3Communicator {
		return NewS3Communicator(server)
	}

	t.Run("unreadable body", func(t *testing.T) {
		req := &http.Request{Header: http.Header{}, Body: erroringBody{}, ContentLength: 10}
		err := newComm().bufferUnsignedPayload(req)
		if err == nil || !errors.Is(err, syscall.EBADMSG) {
			t.Fatalf("expected EBADMSG for an unreadable body, got %v", err)
		}
	})

	t.Run("short body", func(t *testing.T) {
		req := &http.Request{
			Header:        http.Header{},
			Body:          io.NopCloser(strings.NewReader("short")),
			ContentLength: 40,
		}
		err := newComm().bufferUnsignedPayload(req)
		if err == nil || !errors.Is(err, syscall.EBADMSG) {
			t.Fatalf("expected EBADMSG for a short body, got %v", err)
		}
	})

	t.Run("unusable spill directory", func(t *testing.T) {
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		body := "spill me"
		req := &http.Request{
			Header:        http.Header{},
			Body:          io.NopCloser(strings.NewReader(body)),
			ContentLength: int64(len(body)),
		}
		err := newComm().bufferUnsignedPayload(req)
		if err == nil || !errors.Is(err, syscall.EIO) {
			t.Fatalf("expected EIO when the spill cannot be created, got %v", err)
		}
	})
}

// failingConn fails every write so the response-write branches are exercised.
type failingConn struct{ net.Conn }

func (failingConn) Write([]byte) (int, error)        { return 0, syscall.EPIPE }
func (failingConn) SetWriteDeadline(time.Time) error { return nil }
func (failingConn) SetReadDeadline(time.Time) error  { return nil }

// TestS3ExternalResponseWriteFailures proves a dead peer surfaces as EPIPE from
// the interim-suppressed responses instead of a silent success.
func TestS3ExternalResponseWriteFailures(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	comm := NewS3Communicator(server)
	comm.conn = failingConn{server}
	comm.isExternalClient = true
	comm.meta.Hash = strings.Repeat("cd", sha256.Size)

	if err := comm.SendACK(0); err == nil || !errors.Is(err, syscall.EPIPE) {
		t.Fatalf("expected EPIPE from a failed S3 response write, got %v", err)
	}
	comm.writeStreamingError(http.StatusBadRequest, "InvalidArgument", "test", "")
}
