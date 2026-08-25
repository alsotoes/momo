package transport

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// crc32Base64 returns the base64 of the 4-byte big-endian IEEE CRC32 of b, the
// encoding S3 uses for x-amz-checksum-crc32.
func crc32Base64(b []byte) string {
	v := crc32.ChecksumIEEE(b)
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	return base64.StdEncoding.EncodeToString(buf[:])
}

func TestParseChecksum(t *testing.T) {
	mk := func(headers map[string]string) *http.Request {
		req, _ := http.NewRequest(http.MethodPut, "http://127.0.0.1/key", nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return req
	}

	tests := []struct {
		name     string
		hdr      map[string]string
		wantAlgo string
		wantVal  string
		wantErr  bool
	}{
		{"none", map[string]string{}, "", "", false},
		{"explicit algo only", map[string]string{"X-Amz-Checksum-Algorithm": "SHA256"}, "sha256", "", false},
		{"value header", map[string]string{"X-Amz-Checksum-Sha256": "YWJj"}, "sha256", "YWJj", false},
		{"both agree", map[string]string{"X-Amz-Checksum-Algorithm": "CRC32", "X-Amz-Checksum-Crc32": "dGVzdA=="}, "crc32", "dGVzdA==", false},
		{"crc32c", map[string]string{"X-Amz-Checksum-Crc32c": "dGVzdA=="}, "crc32c", "dGVzdA==", false},
		{"sha1", map[string]string{"X-Amz-Checksum-Sha1": "dGVzdA=="}, "sha1", "dGVzdA==", false},
		{"unknown algo", map[string]string{"X-Amz-Checksum-Algorithm": "MD5"}, "", "", true},
		{"conflict", map[string]string{"X-Amz-Checksum-Algorithm": "SHA1", "X-Amz-Checksum-Sha256": "dGVzdA=="}, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			algo, val, err := parseChecksum(mk(tt.hdr))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %+v", tt.hdr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if algo != tt.wantAlgo {
				t.Errorf("algo = %q, want %q", algo, tt.wantAlgo)
			}
			if val != tt.wantVal {
				t.Errorf("value = %q, want %q", val, tt.wantVal)
			}
		})
	}
}

func TestS3ChecksumExpectations(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	comm := NewS3Communicator(serverConn)
	t.Run("value present", func(t *testing.T) {
		comm.meta.S3Headers = map[string]string{
			"x-amz-checksum-algorithm": "sha256",
			"x-amz-checksum-sha256":    "YWJj",
		}
		refs := comm.ChecksumExpectations()
		if len(refs) != 1 || refs[0].Algorithm != "sha256" || refs[0].Value != "YWJj" {
			t.Fatalf("expected one sha256 ref, got %#v", refs)
		}
	})
	t.Run("compute-only no value", func(t *testing.T) {
		comm.meta.S3Headers = map[string]string{"x-amz-checksum-algorithm": "crc32"}
		if refs := comm.ChecksumExpectations(); refs != nil {
			t.Fatalf("expected nil expectations for compute-only, got %#v", refs)
		}
	})
	t.Run("none", func(t *testing.T) {
		comm.meta.S3Headers = nil
		if refs := comm.ChecksumExpectations(); refs != nil {
			t.Fatalf("expected nil expectations, got %#v", refs)
		}
	})
}

func TestS3ChecksumMismatchHook_WritesBadDigest(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	respCh := make(chan string, 1)
	go func() {
		respCh <- readUntilDeadline(t, clientConn, 3*time.Second)
	}()

	comm := NewS3Communicator(serverConn)
	comm.meta.Name = "k.txt"
	if err := comm.OnIntegrityChecksumMismatch(); err == nil {
		t.Fatal("expected mismatch error, got nil")
	}

	resp := <-respCh
	if !strings.Contains(resp, "HTTP/1.1 400") || !strings.Contains(resp, "BadDigest") {
		t.Errorf("expected 400 BadDigest, got %q", resp)
	}
}

func TestS3Checksum_GetChecksumMode(t *testing.T) {
	body := "momoscrc32check"
	mock := &mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			return io.NopCloser(strings.NewReader(body)), common.FileMetadata{
				Name: name, Hash: "abc", Size: int64(len(body)), ModTime: time.Now().UnixNano(),
			}, nil
		},
	}
	req := "GET /test-bucket/obj.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6\r\n" + // notsecret
		"X-Amz-Checksum-Mode: ENABLED\r\n" +
		"Content-Length: 0\r\n\r\n"
	resp, serverErr := runS3RequestCapture(t, req, mock)
	if serverErr != ErrRequestHandled {
		t.Fatalf("expected ErrRequestHandled, got %v", serverErr)
	}
	wantHdr := "x-amz-checksum-crc32: " + crc32Base64([]byte(body))
	if !strings.Contains(resp, wantHdr) {
		t.Errorf("expected GET checksum header %q in response:\n%s", wantHdr, truncate(resp, 800))
	}
}

func TestS3Checksum_UnknownAlgorithmInvalidRequest(t *testing.T) {
	mock := &mockStore{}
	req := "PUT /test-bucket/obj.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6\r\n" + // notsecret
		"X-Amz-Checksum-Algorithm: MD5\r\n" +
		"Content-Length: 0\r\n" +
		"X-Amz-Content-Sha256: abc\r\n\r\n"
	resp, serverErr := runS3RequestCapture(t, req, mock)
	if serverErr == nil {
		t.Fatal("expected rejection error for unknown checksum algorithm")
	}
	if !strings.Contains(resp, "InvalidRequest") {
		t.Errorf("expected InvalidRequest, got:\n%s", truncate(resp, 800))
	}
}

func TestS3Checksum_MultipartCompleteBadDigest(t *testing.T) {
	auth := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	host := "127.0.0.1:4440"

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	comm := NewS3Communicator(server)
	comm.SetStore(&mockStore{})

	done := make(chan error, 1)
	go func() {
		_, _, err := comm.HandshakeServer([]byte(common.PadString(auth, common.AuthTokenLength)))
		done <- err
	}()

	body := []byte("part-one")
	uploadID := "uuid-555"
	upload := &multipartUpload{bucket: "test-bucket", key: "obj.txt", parts: []multipartPart{{partNumber: 1, data: body}}}
	muUploads.Lock()
	uploads[uploadID] = upload
	muUploads.Unlock()

	// CompleteMultipartUpload with a wrong SHA256 checksum -> BadDigest.
	// Read the server's response concurrently so net.Pipe writes do not block.
	respCh := make(chan string, 1)
	go func() {
		respCh <- readUntilDeadline(t, client, 3*time.Second)
	}()

	req := fmt.Sprintf("POST /test-bucket/obj.txt?uploadId=%s HTTP/1.1\r\n"+
		"Host: %s\r\nAuthorization: Bearer %s\r\n"+
		"X-Amz-Checksum-Sha256: %s\r\n"+
		"Content-Length: 0\r\n\r\n",
		uploadID, host, auth, base64.StdEncoding.EncodeToString([]byte("notthechecksum")))
	if _, err := client.Write([]byte(req)); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	resp := <-respCh
	if !strings.Contains(resp, "HTTP/1.1 400") || !strings.Contains(resp, "BadDigest") {
		t.Errorf("expected 400 BadDigest, got:\n%s", truncate(resp, 800))
	}
	<-done
}

// readUntilDeadline reads from c until a read deadline fires (net.Pipe has no
// EOF), returning every byte read so far — sufficient to capture the HTTP
// response headers plus XML error body.
func readUntilDeadline(t *testing.T, c net.Conn, d time.Duration) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(d))
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := c.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

func TestS3Checksum_CollectHeaders(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPut, "http://127.0.0.1/key", nil)
	req.Header.Set("X-Amz-Checksum-Sha256", "YWJjZA==")
	req.Header.Set("X-Amz-Checksum-Algorithm", "SHA256")
	hdrs := collectS3Headers(req)
	if hdrs["x-amz-checksum-sha256"] != "YWJjZA==" {
		t.Errorf("expected persisted checksum header, got %v", hdrs)
	}
	if hdrs["x-amz-checksum-algorithm"] != "sha256" {
		t.Errorf("expected normalized algorithm marker, got %v", hdrs["x-amz-checksum-algorithm"])
	}
}
