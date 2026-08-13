package transport

import (
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

func TestS3MultipartUpload_FullFlow(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "test-token-11111111111111111111111111111111111111111111111111111" // notsecret
	addr := "127.0.0.1:45905"

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			Protocol:    "s3-tcp",
			AuthToken:   authToken,
			TLSInsecure: true,
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen(addr)
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer l.Close()

	mock := &mockStore{
		putFunc: func(name, hash string, size int64, _ string, content io.Reader) error {
			data, _ := io.ReadAll(content)
			if int64(len(data)) != size {
				return fmt.Errorf("size mismatch: got %d, want %d", len(data), size)
			}
			return nil
		},
	}

	startServer(t, l, authToken, mock, 5)

	// Step 1: CreateMultipartUpload
	createReq := fmt.Sprintf("POST /test-bucket/testfile.txt?uploads HTTP/1.1\r\nHost: 127.0.0.1:45905\r\nX-Amz-Date: %s\r\nAuthorization: Bearer %s\r\nContent-Length: 0\r\n\r\n",
		time.Now().UTC().Format("20060102T150405Z"), authToken)
	resp := doS3Request(t, addr, createReq)
	if !strings.Contains(resp, "200 OK") {
		t.Fatalf("CreateMultipartUpload expected 200, got: %s", truncate(resp, 200))
	}
	uploadID := extractXMLTag(resp, "UploadId")
	if uploadID == "" {
		t.Fatalf("CreateMultipartUpload response missing UploadId: %s", truncate(resp, 200))
	}
	t.Logf("Created upload: %s", uploadID)

	// Step 2: UploadPart (2 parts)
	for i, body := range []string{"part1 data content", "part2 data content longer"} {
		partNum := i + 1
		putReq := fmt.Sprintf("PUT /test-bucket/testfile.txt?uploadId=%s&partNumber=%d HTTP/1.1\r\nHost: 127.0.0.1:45905\r\nX-Amz-Date: %s\r\nAuthorization: Bearer %s\r\nContent-Length: %d\r\n\r\n%s",
			uploadID, partNum, time.Now().UTC().Format("20060102T150405Z"), authToken, len(body), body)
		resp := doS3Request(t, addr, putReq)
		if !strings.Contains(resp, "200 OK") {
			t.Fatalf("UploadPart %d expected 200, got: %s", partNum, truncate(resp, 200))
		}
	}

	// Step 3: ListParts
	listPartsReq := fmt.Sprintf("GET /test-bucket/testfile.txt?uploadId=%s HTTP/1.1\r\nHost: 127.0.0.1:45905\r\nX-Amz-Date: %s\r\nAuthorization: Bearer %s\r\n\r\n",
		uploadID, time.Now().UTC().Format("20060102T150405Z"), authToken)
	resp = doS3Request(t, addr, listPartsReq)
	if !strings.Contains(resp, "200 OK") {
		t.Fatalf("ListParts expected 200, got: %s", truncate(resp, 200))
	}
	if !strings.Contains(resp, "PartNumber>1<") || !strings.Contains(resp, "PartNumber>2<") {
		t.Fatalf("ListParts missing parts: %s", truncate(resp, 300))
	}

	// Step 4: CompleteMultipartUpload
	completeXML := `<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>"etag1"</ETag></Part><Part><PartNumber>2</PartNumber><ETag>"etag2"</ETag></Part></CompleteMultipartUpload>`
	completeReq := fmt.Sprintf("POST /test-bucket/testfile.txt?uploadId=%s HTTP/1.1\r\nHost: 127.0.0.1:45905\r\nX-Amz-Date: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/xml\r\nContent-Length: %d\r\n\r\n%s",
		uploadID, time.Now().UTC().Format("20060102T150405Z"), authToken, len(completeXML), completeXML)
	resp = doS3Request(t, addr, completeReq)
	if !strings.Contains(resp, "200 OK") {
		t.Fatalf("CompleteMultipartUpload expected 200, got: %s", truncate(resp, 200))
	}
}

func TestS3MultipartUpload_Abort(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "test-token-11111111111111111111111111111111111111111111111111111"
	addr := "127.0.0.1:45906"

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			Protocol:    "s3-tcp",
			AuthToken:   authToken,
			TLSInsecure: true,
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen(addr)
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer l.Close()

	mock := &mockStore{
		getHashForNameFunc: func(name string) (string, error) {
			return "", fmt.Errorf("not found")
		},
	}
	startServer(t, l, authToken, mock, 3)

	// Create upload
	createReq := fmt.Sprintf("POST /test-bucket/testfile.txt?uploads HTTP/1.1\r\nHost: 127.0.0.1:45906\r\nX-Amz-Date: %s\r\nAuthorization: Bearer %s\r\nContent-Length: 0\r\n\r\n",
		time.Now().UTC().Format("20060102T150405Z"), authToken)
	resp := doS3Request(t, addr, createReq)
	uploadID := extractXMLTag(resp, "UploadId")

	// Abort
	abortReq := fmt.Sprintf("DELETE /test-bucket/testfile.txt?uploadId=%s HTTP/1.1\r\nHost: 127.0.0.1:45906\r\nAuthorization: Bearer %s\r\n\r\n",
		uploadID, authToken)
	resp = doS3Request(t, addr, abortReq)
	if !strings.Contains(resp, "204 No Content") {
		t.Fatalf("AbortMultipartUpload expected 204, got: %s", truncate(resp, 200))
	}

	// Verify upload is gone
	listReq := fmt.Sprintf("GET /test-bucket/testfile.txt?uploadId=%s HTTP/1.1\r\nHost: 127.0.0.1:45906\r\nAuthorization: Bearer %s\r\n\r\n",
		uploadID, authToken)
	resp = doS3Request(t, addr, listReq)
	if !strings.Contains(resp, "404") {
		t.Fatalf("ListParts after abort expected 404, got: %s", truncate(resp, 200))
	}
}

// startServer runs a goroutine that accepts N connections and handles them.
func startServer(t *testing.T, l MomoListener, authToken string, mock storage.Store, n int) {
	t.Helper()
	go func() {
		for i := 0; i < n; i++ {
			comm, err := l.Accept()
			if err != nil {
				return
			}
			if mock != nil {
				if s3Comm, ok := comm.(interface{ SetStore(storage.Store) }); ok {
					s3Comm.SetStore(mock)
				}
			}
			_, _, err = comm.HandshakeServer([]byte(common.PadString(authToken, common.AuthTokenLength)))
			comm.Close()
			if err != ErrRequestHandled {
				t.Logf("server handler returned unexpected error: %v", err)
			}
		}
	}()
	time.Sleep(100 * time.Millisecond)
}

// ─── Helpers ──────────────────────────────────────────────────────────────

func doS3Request(t *testing.T, addr, rawReq string) string {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(rawReq)); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	resp, err := io.ReadAll(io.LimitReader(conn, 65536))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	return string(resp)
}

func extractXMLTag(xmlStr, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(xmlStr, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(xmlStr[start:], close)
	if end < 0 {
		return ""
	}
	return xmlStr[start : start+end]
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
