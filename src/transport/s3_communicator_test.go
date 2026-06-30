package transport

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"go.uber.org/goleak"
)

func verifyNoLeaks(t *testing.T) {
	goleak.VerifyNone(t,
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*Transport).runSendQueue"),
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*Transport).listen"),
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*Conn).run"),
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*sendQueue).Run"),
	)
}

func TestS3Communicator_HandshakeServer(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // not a real token
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))

	reqBody := "PUT /test-file.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n" +
		"X-Amz-Date: 20260604T120000Z\r\n" +
		"X-Amz-Content-Sha256: dummyhash\r\n" +
		"Content-Length: 1024\r\n\r\n"

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		clientConn.Write([]byte(reqBody))
		// ⚡ Bolt: Read in a loop to avoid deadlock on net.Pipe. 
		// http.Response.Write performs multiple writes which will block if not fully consumed.
		buf := make([]byte, 1024)
		for {
			_, err := clientConn.Read(buf)
			if err != nil {
				break
			}
		}
	}()

	comm := NewS3Communicator(serverConn)
	_, timestamp, err := comm.HandshakeServer(expectedAuthToken)
	if err != nil {
		t.Fatalf("HandshakeServer failed: %v", err)
	}

	if timestamp == 0 {
		t.Errorf("Expected non-zero timestamp from X-Amz-Date")
	}

	meta, err := comm.ReceiveMetadata()
	if err != nil {
		t.Fatalf("ReceiveMetadata failed: %v", err)
	}

	if err := comm.SendMetadataStatus(MetadataStatusSendPayload); err != nil {
		t.Fatalf("SendMetadataStatus failed: %v", err)
	}

	if meta.Size != 1024 {
		t.Errorf("Expected size 1024, got %d", meta.Size)
	}
	expectedName := "test-file.txt"
	if meta.Name != expectedName {
		t.Errorf("Expected name %q, got %q", expectedName, meta.Name)
	}
}

func TestS3Communicator_AWSV4Auth(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // not a real token
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))

	// AWS v4 style Auth header
	authHeader := "AWS4-HMAC-SHA256 Credential=" + authToken + "/20260604/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=dummy"

	reqBody := "PUT /test-file2.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: " + authHeader + "\r\n" +
		"X-Amz-Date: 20260604T120000Z\r\n" +
		"X-Amz-Content-Sha256: dummyhash\r\n" +
		"Content-Length: 2048\r\n\r\n"

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		clientConn.Write([]byte(reqBody))
		// ⚡ Bolt: Read in a loop to avoid deadlock on net.Pipe. 
		// http.Response.Write performs multiple writes which will block if not fully consumed.
		buf := make([]byte, 1024)
		for {
			_, err := clientConn.Read(buf)
			if err != nil {
				break
			}
		}
	}()

	comm := NewS3Communicator(serverConn)
	_, _, err := comm.HandshakeServer(expectedAuthToken)
	if err != nil {
		t.Fatalf("HandshakeServer failed with AWS v4 auth: %v", err)
	}
}

func TestS3Communicator_HashTraversalValidation(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // not a real token
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))

	maliciousHashes := []string{
		"../../malicious",
		"some/path",
		"bad\\hash",
		".dot",
	}

	for _, malHash := range maliciousHashes {
		t.Run("hash_"+malHash, func(t *testing.T) {
			reqBody := "PUT /test-file.txt HTTP/1.1\r\n" +
				"Host: 127.0.0.1:4440\r\n" +
				"Authorization: Bearer " + authToken + "\r\n" +
				"X-Amz-Date: 20260604T120000Z\r\n" +
				"X-Amz-Content-Sha256: " + malHash + "\r\n" +
				"Content-Length: 1024\r\n\r\n"

			clientConn, serverConn := net.Pipe()
			defer clientConn.Close()
			defer serverConn.Close()

			go func() {
				clientConn.Write([]byte(reqBody))
				buf := make([]byte, 1024)
				for {
					_, err := clientConn.Read(buf)
					if err != nil {
						break
					}
				}
			}()

			comm := NewS3Communicator(serverConn)
			_, _, err := comm.HandshakeServer(expectedAuthToken)
			if err == nil {
				t.Fatalf("Expected HandshakeServer to fail on malicious hash %q, but got success", malHash)
			}
			if !strings.Contains(err.Error(), "invalid hash") {
				t.Errorf("Expected invalid hash error, got %v", err)
			}
			// Verify POSIX error mapping to syscall.EBADMSG
			if !errors.Is(err, syscall.EBADMSG) {
				t.Errorf("Expected error to wrap syscall.EBADMSG, got %v", err)
			}
		})
	}
}

func TestS3Communicator_EdgeCases(t *testing.T) {
	defer verifyNoLeaks(t)

	// 1. Panic recovery tests (Rule 4) via nil communicator
	var nilComm *S3Communicator
	
	_, err := nilComm.Read(make([]byte, 10))
	if err == nil {
		t.Errorf("Expected Read on nilComm to fail")
	}

	_, err = nilComm.Write(make([]byte, 10))
	if err == nil {
		t.Errorf("Expected Write on nilComm to fail")
	}

	err = nilComm.Close()
	if err == nil {
		t.Errorf("Expected Close on nilComm to fail")
	}

	err = nilComm.SetAbsoluteDeadline(time.Now())
	if err == nil {
		t.Errorf("Expected SetAbsoluteDeadline on nilComm to fail")
	}

	_, err = nilComm.HandshakeClient("token", 12345, 1)
	if err == nil {
		t.Errorf("Expected HandshakeClient on nilComm to fail")
	}

	_, _, err = nilComm.HandshakeServer([]byte("token"))
	if err == nil {
		t.Errorf("Expected HandshakeServer on nilComm to fail")
	}

	err = nilComm.SendReplicationMode(1)
	if err == nil {
		t.Errorf("Expected SendReplicationMode on nilComm to fail")
	}

	_, err = nilComm.SendMetadata(&common.FileMetadata{})
	if err == nil {
		t.Errorf("Expected SendMetadata on nilComm to fail")
	}

	_, err = nilComm.ReceiveMetadata()
	if err == nil {
		t.Errorf("Expected ReceiveMetadata on nilComm to fail")
	}

	err = nilComm.SendMetadataStatus(1)
	if err == nil {
		t.Errorf("Expected SendMetadataStatus on nilComm to fail")
	}

	err = nilComm.SendACK(0)
	if err == nil {
		t.Errorf("Expected SendACK on nilComm to fail")
	}

	err = nilComm.ReceiveACK()
	if err == nil {
		t.Errorf("Expected ReceiveACK on nilComm to fail")
	}
}

type mockStore struct {
	putFunc     func(name string, hash string, size int64, remotePath string, content io.Reader) error
	getFunc     func(name string) (io.ReadCloser, common.FileMetadata, error)
	hasFunc     func(hash string) (bool, error)
	deleteFunc  func(name string) error
	listFunc    func() ([]common.FileMetadata, error)
	getBlobPath func(name string) (string, error)
}

func (m *mockStore) Close() error { return nil }
func (m *mockStore) Put(name string, hash string, size int64, remotePath string, content io.Reader) error {
	if m.putFunc != nil {
		return m.putFunc(name, hash, size, remotePath, content)
	}
	return nil
}
func (m *mockStore) Get(name string) (io.ReadCloser, common.FileMetadata, error) {
	if m.getFunc != nil {
		return m.getFunc(name)
	}
	return nil, common.FileMetadata{}, syscall.ENOENT
}
func (m *mockStore) Has(hash string) (bool, error) {
	if m.hasFunc != nil {
		return m.hasFunc(hash)
	}
	return false, nil
}
func (m *mockStore) Delete(name string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(name)
	}
	return nil
}
func (m *mockStore) List() ([]common.FileMetadata, error) {
	if m.listFunc != nil {
		return m.listFunc()
	}
	return nil, nil
}
func (m *mockStore) GetBlobPath(name string) (string, error) {
	if m.getBlobPath != nil {
		return m.getBlobPath(name)
	}
	return "", nil
}

func runS3TestRequest(t *testing.T, reqStr string, mock storage.Store) string {
	expectedAuthToken := []byte(common.PadString("a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6", common.AuthTokenLength))

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer l.Close()

	addr := l.Addr().String()
	errChan := make(chan error, 1)

	// Server goroutine
	go func() {
		conn, err := l.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer conn.Close()

		comm := NewS3Communicator(conn)
		comm.SetStore(mock)

		_, _, err = comm.HandshakeServer(expectedAuthToken)
		errChan <- err
	}()

	// Client goroutine
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte(reqStr))
	if err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}

	// Shutdown write half so server sees EOF if needed, but keep read open
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.CloseWrite()
	}

	respBytes, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	serverErr := <-errChan
	if serverErr != ErrRequestHandled {
		t.Fatalf("Server expected ErrRequestHandled, got: %v", serverErr)
	}

	return string(respBytes)
}

func TestS3Communicator_URLParsing(t *testing.T) {
	tests := []struct {
		name           string
		host           string
		path           string
		expectedBucket string
		expectedKey    string
	}{
		{
			name:           "Path style bucket and key",
			host:           "localhost:4440",
			path:           "/mybucket/myfolder/file.txt",
			expectedBucket: "mybucket",
			expectedKey:    "myfolder/file.txt",
		},
		{
			name:           "Virtual host style bucket with key",
			host:           "mybucket.s3.amazonaws.com",
			path:           "/myfolder/file.txt",
			expectedBucket: "mybucket",
			expectedKey:    "myfolder/file.txt",
		},
		{
			name:           "Virtual host style localhost",
			host:           "mybucket.localhost",
			path:           "/file.txt",
			expectedBucket: "mybucket",
			expectedKey:    "file.txt",
		},
		{
			name:           "Virtual host style bucket root",
			host:           "mybucket.s3.us-east-1.amazonaws.com",
			path:           "/",
			expectedBucket: "mybucket",
			expectedKey:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "http://"+tc.host+tc.path, nil)
			req.Host = tc.host
			b, k := extractS3BucketAndKey(req)
			if b != tc.expectedBucket {
				t.Errorf("Expected bucket %q, got %q", tc.expectedBucket, b)
			}
			if k != tc.expectedKey {
				t.Errorf("Expected key %q, got %q", tc.expectedKey, k)
			}
		})
	}
}

func TestS3Communicator_XMLFormatting(t *testing.T) {
	files := []common.FileMetadata{
		{Name: "file1.txt", Hash: "hash1", Size: 100, RemotePath: ""},
		{Name: "file2.txt", Hash: "hash2", Size: 200, RemotePath: "docs"},
		{Name: "file3.txt", Hash: "hash3", Size: 300, RemotePath: "docs/nested"},
	}

	// 1. Root listing (prefix: "", delimiter: "")
	xmlBytes := FormatListObjectsV2XML("mybucket", "", "", 1000, files)
	xmlStr := string(xmlBytes)

	if !strings.Contains(xmlStr, "<Name>mybucket</Name>") {
		t.Errorf("Expected bucket name in XML")
	}
	if !strings.Contains(xmlStr, "<Key>file1.txt</Key>") {
		t.Errorf("Expected file1.txt in XML")
	}
	if !strings.Contains(xmlStr, "<Key>docs/file2.txt</Key>") {
		t.Errorf("Expected docs/file2.txt in XML")
	}
	if !strings.Contains(xmlStr, "<Key>docs/nested/file3.txt</Key>") {
		t.Errorf("Expected docs/nested/file3.txt in XML")
	}
	if strings.Contains(xmlStr, "<CommonPrefixes>") {
		t.Errorf("Did not expect CommonPrefixes in flat listing")
	}

	// 2. Prefix and delimiter grouping
	xmlBytesDelim := FormatListObjectsV2XML("mybucket", "", "/", 1000, files)
	xmlStrDelim := string(xmlBytesDelim)

	if !strings.Contains(xmlStrDelim, "<Key>file1.txt</Key>") {
		t.Errorf("Expected file1.txt at root")
	}
	if strings.Contains(xmlStrDelim, "<Key>docs/file2.txt</Key>") {
		t.Errorf("Did not expect file2.txt inside CommonPrefix group in flat section")
	}
	if !strings.Contains(xmlStrDelim, "<Prefix>docs/</Prefix>") {
		t.Errorf("Expected docs/ as CommonPrefix")
	}
}

func TestS3Communicator_GET_ListObjectsV2(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
	reqStr := "GET /?list-type=2 HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"

	mock := &mockStore{
		listFunc: func() ([]common.FileMetadata, error) {
			return []common.FileMetadata{
				{Name: "test-file.txt", Hash: "hash123", Size: 500},
			}, nil
		},
	}

	respStr := runS3TestRequest(t, reqStr, mock)

	if !strings.Contains(respStr, "HTTP/1.1 200 OK") {
		t.Errorf("Expected 200 OK, got: %s", respStr)
	}
	if !strings.Contains(respStr, "Content-Type: application/xml") {
		t.Errorf("Expected XML content type, got: %s", respStr)
	}
	if !strings.Contains(respStr, "<Key>test-file.txt</Key>") {
		t.Errorf("Expected test-file.txt in body, got: %s", respStr)
	}
}

func TestS3Communicator_GET_GetObject(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
	reqStr := "GET /bucket/hello.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"

	fileContent := []byte("hello s3 download!")
	mock := &mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			if name != "hello.txt" {
				return nil, common.FileMetadata{}, syscall.ENOENT
			}
			return io.NopCloser(bytes.NewReader(fileContent)), common.FileMetadata{
				Name: "hello.txt",
				Size: int64(len(fileContent)),
			}, nil
		},
	}

	respStr := runS3TestRequest(t, reqStr, mock)

	if !strings.Contains(respStr, "HTTP/1.1 200 OK") {
		t.Errorf("Expected 200 OK, got: %s", respStr)
	}
	if !strings.Contains(respStr, "hello s3 download!") {
		t.Errorf("Expected streamed body inside response, got: %s", respStr)
	}
}

func TestS3Communicator_DELETE(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
	reqStr := "DELETE /bucket/mydeletedfile.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"

	deletedKey := ""
	mock := &mockStore{
		deleteFunc: func(name string) error {
			deletedKey = name
			return nil
		},
	}

	respStr := runS3TestRequest(t, reqStr, mock)

	if deletedKey != "mydeletedfile.txt" {
		t.Errorf("Expected store.Delete to be called with 'mydeletedfile.txt', got %q", deletedKey)
	}

	if !strings.Contains(respStr, "HTTP/1.1 204 No Content") {
		t.Errorf("Expected 204 No Content, got: %s", respStr)
	}
}
