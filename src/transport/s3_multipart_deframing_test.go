package transport

import (
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// newPipeComm returns a communicator backed by a pipe the caller can close.
func newPipeComm(t *testing.T) (*S3Communicator, net.Conn) { //nolint:unparam // the caller closes the pipe via t.Cleanup
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return NewS3Communicator(server), client
}

// TestMultipartAWSChunkedPartStoresDecodedBytes is the regression test for issue
// #1358: a part sent with aws-chunked framing and a checksum trailer must be
// stored as its decoded bytes. Previously the framing was stored verbatim, so a
// 20 MiB upload landed as 20 MiB + 162 bytes and its checksum never validated.
func TestMultipartAWSChunkedPartStoresDecodedBytes(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "deframe-token-1111111111111111111111111111111111111111111111" // notsecret
	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			Protocol:    "s3-tcp",
			AuthToken:   authToken,
			TLSInsecure: true,
		},
	}
	factory := NewProtocolFactory(cfg)
	l, err := factory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()
	addr := l.Addr().String()

	var storedSize int64
	var stored []byte
	mock := &mockStore{
		putFunc: func(_ string, _ string, size int64, _ string, content io.Reader) error {
			data, rerr := io.ReadAll(content)
			if rerr != nil {
				return rerr
			}
			storedSize, stored = size, data
			if int64(len(data)) != size {
				return fmt.Errorf("size mismatch: got %d, want %d", len(data), size)
			}
			return nil
		},
	}
	startServer(t, l, authToken, mock, 5)

	now := time.Now().UTC().Format("20060102T150405Z")
	createReq := fmt.Sprintf("POST /test-bucket/chunked.txt?uploads HTTP/1.1\r\nHost: %s\r\nX-Amz-Date: %s\r\nAuthorization: Bearer %s\r\nContent-Length: 0\r\n\r\n",
		addr, now, authToken)
	createResp := doS3Request(t, addr, createReq)
	uploadID := extractXMLTag(createResp, "UploadId")
	if uploadID == "" {
		t.Fatalf("no UploadId: %s", truncate(createResp, 200))
	}

	// Part 1 is aws-chunked with a checksum trailer; part 2 is plain, so both
	// paths are covered by one completion.
	payload1 := "chunked-part-payload"
	framed := fmt.Sprintf("%x\r\n%s\r\n0\r\nx-amz-checksum-crc32:AAAAAA==\r\n\r\n", len(payload1), payload1)
	part1Req := fmt.Sprintf("PUT /test-bucket/chunked.txt?uploadId=%s&partNumber=1 HTTP/1.1\r\n"+
		"Host: %s\r\nX-Amz-Date: %s\r\nAuthorization: Bearer %s\r\n"+
		"Content-Encoding: aws-chunked\r\n"+
		"X-Amz-Content-Sha256: %s\r\n"+
		"Content-Length: %d\r\n\r\n%s",
		uploadID, addr, now, authToken, s3StreamingUnsignedTrailer, len(framed), framed)
	if resp := doS3Request(t, addr, part1Req); !strings.Contains(resp, "200 OK") {
		t.Fatalf("chunked UploadPart expected 200, got: %s", truncate(resp, 200))
	}

	payload2 := "-plain-part"
	part2Req := fmt.Sprintf("PUT /test-bucket/chunked.txt?uploadId=%s&partNumber=2 HTTP/1.1\r\n"+
		"Host: %s\r\nX-Amz-Date: %s\r\nAuthorization: Bearer %s\r\nContent-Length: %d\r\n\r\n%s",
		uploadID, addr, now, authToken, len(payload2), payload2)
	if resp := doS3Request(t, addr, part2Req); !strings.Contains(resp, "200 OK") {
		t.Fatalf("plain UploadPart expected 200, got: %s", truncate(resp, 200))
	}

	completeXML := `<CompleteMultipartUpload><Part><PartNumber>1</PartNumber></Part><Part><PartNumber>2</PartNumber></Part></CompleteMultipartUpload>`
	completeReq := fmt.Sprintf("POST /test-bucket/chunked.txt?uploadId=%s HTTP/1.1\r\n"+
		"Host: %s\r\nX-Amz-Date: %s\r\nAuthorization: Bearer %s\r\n"+
		"Content-Type: application/xml\r\nContent-Length: %d\r\n\r\n%s",
		uploadID, addr, now, authToken, len(completeXML), completeXML)
	if resp := doS3Request(t, addr, completeReq); !strings.Contains(resp, "200 OK") {
		t.Fatalf("Complete expected 200, got: %s", truncate(resp, 240))
	}

	want := payload1 + payload2
	if string(stored) != want {
		t.Fatalf("stored %q, want %q (framing must not reach the object)", stored, want)
	}
	if storedSize != int64(len(want)) {
		t.Fatalf("stored size = %d, want %d", storedSize, len(want))
	}
}

// TestS3RangeGetOmitsChecksumHeaders proves a 206 carries no x-amz-checksum-*:
// a whole-object checksum cannot describe a partial body, and clients validate
// per range and abort with a mismatch otherwise (issue #1354).
func TestS3RangeGetOmitsChecksumHeaders(t *testing.T) {
	defer verifyNoLeaks(t)
	req := s3GetRequest(s3ReqAuth, "obj.txt",
		"Range: bytes=4-9\r\nx-amz-checksum-mode: ENABLED\r\n")

	resp := runS3TestRequest(t, req, rangeMockStore())
	if !strings.Contains(resp, "HTTP/1.1 206 Partial Content") {
		t.Fatalf("expected 206, got: %s", resp)
	}
	if strings.Contains(resp, "x-amz-checksum-") {
		t.Fatalf("206 response must not carry a checksum header: %s", resp)
	}
}
