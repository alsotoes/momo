package transport

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
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

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
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

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := "us-east-1"
	payloadHash := "dummyhash"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalRequest := "PUT\n/test-file2.txt\n\nhost:127.0.0.1:4440\nx-amz-content-sha256:" + payloadHash + "\nx-amz-date:" + amzDate + "\n\n" + signedHeaders + "\n" + payloadHash
	stringToSign := buildStringToSign(canonicalRequest, amzDate, dateStamp, region)
	signingKey := deriveSigningKey(authToken, dateStamp, region)
	signature := computeSignature(signingKey, stringToSign)

	authHeader := "AWS4-HMAC-SHA256 Credential=" + authToken + "/" + dateStamp + "/" + region + "/s3/aws4_request, SignedHeaders=" + signedHeaders + ", Signature=" + signature

	reqBody := "PUT /test-file2.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: " + authHeader + "\r\n" +
		"X-Amz-Date: " + amzDate + "\r\n" +
		"X-Amz-Content-Sha256: " + payloadHash + "\r\n" +
		"Content-Length: 2048\r\n\r\n"

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
	if err != nil {
		t.Fatalf("HandshakeServer failed with AWS v4 auth: %v", err)
	}
}

func TestS3Communicator_SigV4InvalidSignature(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))

	authHeader := "AWS4-HMAC-SHA256 Credential=" + authToken + "/20260604/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=" + strings.Repeat("0", 64)

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
		t.Fatal("HandshakeServer should reject invalid SigV4 signature")
	}
	if !errors.Is(err, syscall.EACCES) {
		t.Fatalf("Expected EACCES for invalid signature, got: %v", err)
	}
}

func TestS3Communicator_HashTraversalValidation(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))

	maliciousHashes := []string{
		"../../malicious",
		"some/path",
		"bad\\hash",
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
	putFunc            func(name string, hash string, size int64, remotePath string, content io.Reader) error
	getFunc            func(name string) (io.ReadCloser, common.FileMetadata, error)
	hasFunc            func(hash string) (bool, error)
	getHashForNameFunc func(name string) (string, error)
	deleteFunc         func(name string) error
	listFunc           func() ([]common.FileMetadata, error)
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
func (m *mockStore) GetHashForName(name string) (string, error) {
	if m.getHashForNameFunc != nil {
		return m.getHashForNameFunc(name)
	}
	return "", syscall.ENOENT
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

func runS3RequestCapture(t *testing.T, reqStr string, mock storage.Store) (string, error) {
	return runS3RequestCaptureBucket(t, reqStr, mock, "")
}

func runS3RequestCaptureBucket(t *testing.T, reqStr string, mock storage.Store, configuredBucket string) (string, error) {
	expectedAuthToken := []byte(common.PadString("a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6", common.AuthTokenLength)) // notsecret

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
		comm.SetConfiguredBucket(configuredBucket)

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

	return string(respBytes), serverErr
}

func runS3TestRequest(t *testing.T, reqStr string, mock storage.Store) string {
	respStr, serverErr := runS3RequestCapture(t, reqStr, mock)
	if serverErr != ErrRequestHandled {
		t.Fatalf("Server expected ErrRequestHandled, got: %v", serverErr)
	}
	return respStr
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
			name:           "Virtual host style localhost with port",
			host:           "mybucket.localhost:9000",
			path:           "/file.txt",
			expectedBucket: "mybucket",
			expectedKey:    "file.txt",
		},
		{
			name:           "Virtual host style s3 with port",
			host:           "mybucket.s3.amazonaws.com:9000",
			path:           "/myfolder/file.txt",
			expectedBucket: "mybucket",
			expectedKey:    "myfolder/file.txt",
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

func TestS3Communicator_KeyTraversalValidation(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))

	maliciousKeys := []string{
		"/bucket/../../passwd",
		"/bucket/bad\\key",
	}

	for _, malKey := range maliciousKeys {
		t.Run("key_"+malKey, func(t *testing.T) {
			reqBody := "GET " + malKey + " HTTP/1.1\r\n" +
				"Host: 127.0.0.1:4440\r\n" +
				"Authorization: Bearer " + authToken + "\r\n\r\n"

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
				t.Fatalf("Expected HandshakeServer to fail on malicious key %q, but got success", malKey)
			}
			if !strings.Contains(err.Error(), "invalid key path traversal") {
				t.Errorf("Expected path traversal error, got %v", err)
			}
			if !errors.Is(err, syscall.EBADMSG) {
				t.Errorf("Expected error to wrap syscall.EBADMSG, got %v", err)
			}
		})
	}
}

func TestS3Communicator_ListObjectsV2KeyCount(t *testing.T) {
	files := []common.FileMetadata{
		{Name: "file1.txt", Hash: "hash1", Size: 100},
		{Name: "docs/file2.txt", Hash: "hash2", Size: 200},
		{Name: "docs/nested/file3.txt", Hash: "hash3", Size: 300},
		{Name: "src/file4.go", Hash: "hash4", Size: 400},
	}

	xmlBytes, err := FormatListObjectsV2XML("mybucket", "", "/", 1000, files)
	if err != nil {
		t.Fatalf("FormatListObjectsV2XML failed: %v", err)
	}
	xmlStr := string(xmlBytes)

	contentsCount := strings.Count(xmlStr, "<Contents>")
	prefixesCount := strings.Count(xmlStr, "<CommonPrefixes>")
	if contentsCount != 1 {
		t.Fatalf("Expected 1 Contents entry, got %d", contentsCount)
	}
	if prefixesCount != 2 {
		t.Fatalf("Expected 2 CommonPrefixes (docs/, src/), got %d", prefixesCount)
	}

	re := regexp.MustCompile(`<KeyCount>(\d+)</KeyCount>`)
	m := re.FindStringSubmatch(xmlStr)
	if m == nil {
		t.Fatalf("KeyCount element not found in XML")
	}
	keyCount, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("KeyCount not an integer: %v", err)
	}
	if keyCount != contentsCount {
		t.Errorf("KeyCount (%d) must equal number of Contents entries (%d), excluding CommonPrefixes", keyCount, contentsCount)
	}
}

func TestS3Communicator_XMLFormatting(t *testing.T) {
	files := []common.FileMetadata{
		{Name: "file1.txt", Hash: "hash1", Size: 100, RemotePath: "", ModTime: 1700000000123000000},
		{Name: "docs/file2.txt", Hash: "hash2", Size: 200, RemotePath: "docs", ModTime: 1700000000456000000},
		{Name: "docs/nested/file3.txt", Hash: "hash3", Size: 300, RemotePath: "docs/nested"},
		{Name: "oversized-filename-exceeding-sixty-four-characters-limit-should-be-skipped-entirely.txt", Hash: "hash4", Size: 400},
		{Name: "valid.txt", Hash: "oversized-hash-exceeding-sixty-four-characters-limit-should-be-skipped-entirely", Size: 500},
	}

	// 1. Root listing (prefix: "", delimiter: "")
	xmlBytes, err := FormatListObjectsV2XML("mybucket", "", "", 1000, files)
	if err != nil {
		t.Fatalf("FormatListObjectsV2XML failed: %v", err)
	}
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
	if strings.Contains(xmlStr, "oversized-filename-exceeding") {
		t.Errorf("Did not expect oversized filename in XML")
	}
	if strings.Contains(xmlStr, "oversized-hash-exceeding") {
		t.Errorf("Did not expect oversized hash in XML")
	}

	// 1b. LastModified must reflect the actual ModTime, not a hardcoded value.
	if !strings.Contains(xmlStr, "<LastModified>2023-11-14T22:13:20.123Z</LastModified>") {
		t.Errorf("Expected LastModified derived from ModTime, got: %s", xmlStr)
	}
	if strings.Contains(xmlStr, "2026-06-29T12:00:00.000Z") {
		t.Errorf("Hardcoded LastModified timestamp leaked into XML")
	}

	// 2. Prefix and delimiter grouping
	xmlBytesDelim, err := FormatListObjectsV2XML("mybucket", "", "/", 1000, files)
	if err != nil {
		t.Fatalf("FormatListObjectsV2XML failed: %v", err)
	}
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
	if !strings.Contains(xmlStrDelim, "<KeyCount>1</KeyCount>") {
		t.Errorf("Expected KeyCount to exclude CommonPrefixes (got XML: %s)", xmlStrDelim)
	}

	// 3. Reject input exceeding 64 bytes (Rule 35)
	_, err = FormatListObjectsV2XML("my-very-long-bucket-name-that-exceeds-sixty-four-characters-limit-completely", "", "", 1000, files)
	if !errors.Is(err, syscall.EINVAL) {
		t.Errorf("Expected syscall.EINVAL for oversized bucket name, got %v", err)
	}
}

func TestS3Communicator_GET_ListObjectsV2(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
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

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
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

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
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

func TestS3Communicator_HEAD_HeadObject(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	reqStr := "HEAD /bucket/hello.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"

	fileContent := []byte("hello s3 download!")
	modTime := int64(1700000000123456789)
	mock := &mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			if name != "hello.txt" {
				return nil, common.FileMetadata{}, syscall.ENOENT
			}
			return io.NopCloser(bytes.NewReader(fileContent)), common.FileMetadata{
				Name:    "hello.txt",
				Hash:    "abc123def456",
				Size:    int64(len(fileContent)),
				ModTime: modTime,
			}, nil
		},
	}

	respStr := runS3TestRequest(t, reqStr, mock)

	if !strings.Contains(respStr, "HTTP/1.1 200 OK") {
		t.Errorf("Expected 200 OK, got: %s", respStr)
	}
	if !strings.Contains(respStr, "ETag: \"abc123def456\"") {
		t.Errorf("Expected quoted ETag, got: %s", respStr)
	}
	if !strings.Contains(respStr, "Content-Length: 18") {
		t.Errorf("Expected Content-Length matching object size, got: %s", respStr)
	}
	// Last-Modified must be IMF-fixdate (RFC 7231) so aws-cli/SDKs can parse it.
	if !strings.Contains(respStr, "Last-Modified: Tue, 14 Nov 2023 22:13:20 GMT") {
		t.Errorf("Expected IMF-fixdate Last-Modified, got: %s", respStr)
	}
	if !strings.Contains(respStr, "Content-Type: application/octet-stream") {
		t.Errorf("Expected Content-Type header, got: %s", respStr)
	}
	if !strings.Contains(respStr, "Connection: close") {
		t.Errorf("Expected Connection: close, got: %s", respStr)
	}

	// HEAD must be header-only: no message body after the header terminator.
	headerEnd := strings.Index(respStr, "\r\n\r\n")
	if headerEnd == -1 {
		t.Fatalf("Malformed HTTP response: %s", respStr)
	}
	if body := respStr[headerEnd+4:]; body != "" {
		t.Errorf("HEAD must have no body, got: %q", body)
	}

	// ETag formatting must match GetObject exactly (same metadata source).
	// Both quoted with the hash from store.Get.
	etagMatch := regexp.MustCompile(`ETag: "([^"]*)"`).FindStringSubmatch(respStr)
	if etagMatch == nil || etagMatch[1] != "abc123def456" {
		t.Errorf("Expected ETag to equal store hash, got: %v", etagMatch)
	}
}

func TestS3Communicator_HEAD_MissingObject(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	reqStr := "HEAD /bucket/missing.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"

	mock := &mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			return nil, common.FileMetadata{}, syscall.ENOENT
		},
	}

	respStr := runS3TestRequest(t, reqStr, mock)

	if !strings.Contains(respStr, "HTTP/1.1 404 Not Found") {
		t.Errorf("Expected 404 Not Found, got: %s", respStr)
	}
	if !strings.Contains(respStr, "<Code>NoSuchKey</Code>") {
		t.Errorf("Expected NoSuchKey XML error, got: %s", respStr)
	}
	if !strings.Contains(respStr, "<Resource>missing.txt</Resource>") {
		t.Errorf("Expected missing.txt resource, got: %s", respStr)
	}
}

func TestS3Communicator_HEAD_HeadBucket(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

	// HeadBucket via path-style (key empty).
	reqStr := "HEAD /mybucket HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"

	respStr := runS3TestRequest(t, reqStr, &mockStore{})
	if !strings.Contains(respStr, "HTTP/1.1 200 OK") {
		t.Errorf("Expected 200 OK for HeadBucket, got: %s", respStr)
	}
	if !strings.Contains(respStr, "Content-Length: 0") {
		t.Errorf("Expected zero-length body for HeadBucket, got: %s", respStr)
	}

	// Endpoint liveness check: HEAD / with empty bucket.
	reqStrRoot := "HEAD / HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"

	respStrRoot := runS3TestRequest(t, reqStrRoot, &mockStore{})
	if !strings.Contains(respStrRoot, "HTTP/1.1 200 OK") {
		t.Errorf("Expected 200 OK for HEAD /, got: %s", respStrRoot)
	}

	// Nil store -> 500 InternalError.
	respErr, serverErr := runS3RequestCapture(t, reqStr, nil)
	if serverErr == nil {
		t.Fatal("Expected error for nil store")
	}
	if !strings.Contains(respErr, "HTTP/1.1 500 Internal Server Error") {
		t.Errorf("Expected 500 for nil store, got: %s", respErr)
	}
	if !strings.Contains(respErr, "<Code>InternalError</Code>") {
		t.Errorf("Expected InternalError XML error, got: %s", respErr)
	}
}

func TestS3Communicator_HEAD_SigV4Auth(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := "us-east-1"
	payloadHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" // sha256("")
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalRequest := "HEAD\n/bucket/hello.txt\n\nhost:127.0.0.1:4440\nx-amz-content-sha256:" + payloadHash + "\nx-amz-date:" + amzDate + "\n\n" + signedHeaders + "\n" + payloadHash
	stringToSign := buildStringToSign(canonicalRequest, amzDate, dateStamp, region)
	signingKey := deriveSigningKey(authToken, dateStamp, region)
	signature := computeSignature(signingKey, stringToSign)

	authHeader := "AWS4-HMAC-SHA256 Credential=" + authToken + "/" + dateStamp + "/" + region + "/s3/aws4_request, SignedHeaders=" + signedHeaders + ", Signature=" + signature

	reqBody := "HEAD /bucket/hello.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: " + authHeader + "\r\n" +
		"X-Amz-Date: " + amzDate + "\r\n" +
		"X-Amz-Content-Sha256: " + payloadHash + "\r\n\r\n"

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

	mock := &mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			return io.NopCloser(bytes.NewReader([]byte("data"))), common.FileMetadata{Name: "hello.txt", Hash: "hash1", Size: 4}, nil
		},
	}

	comm := NewS3Communicator(serverConn)
	comm.SetStore(mock)
	_, _, err := comm.HandshakeServer(expectedAuthToken)
	if err != ErrRequestHandled {
		t.Fatalf("Expected ErrRequestHandled for HEAD with SigV4, got: %v", err)
	}
}

func TestFormatListBucketsXML(t *testing.T) {
	empty := string(FormatListBucketsXML(""))
	if !strings.Contains(empty, "<ListAllMyBucketsResult") {
		t.Errorf("Expected ListAllMyBucketsResult root, got: %s", empty)
	}
	if strings.Contains(empty, "<Bucket>") {
		t.Errorf("Expected no buckets for empty configured bucket, got: %s", empty)
	}
	if strings.Contains(empty, "<Name>") {
		t.Errorf("Expected no bucket names for empty configured bucket, got: %s", empty)
	}

	withBucket := string(FormatListBucketsXML("mybucket"))
	if !strings.Contains(withBucket, "<Name>mybucket</Name>") {
		t.Errorf("Expected configured bucket in list, got: %s", withBucket)
	}
	if !strings.Contains(withBucket, "<Owner><ID>momo</ID>") {
		t.Errorf("Expected Owner element, got: %s", withBucket)
	}
}

func TestFormatGetBucketLocationXML(t *testing.T) {
	empty := string(FormatGetBucketLocationXML(""))
	if !strings.Contains(empty, "<LocationConstraint") || !strings.Contains(empty, "</LocationConstraint>") {
		t.Errorf("Expected LocationConstraint element, got: %s", empty)
	}
	if strings.Contains(empty, ">us-east-1<") {
		t.Errorf("Expected empty region for empty input, got: %s", empty)
	}

	region := string(FormatGetBucketLocationXML("us-east-1"))
	if !strings.Contains(region, ">us-east-1</LocationConstraint>") {
		t.Errorf("Expected region inside LocationConstraint, got: %s", region)
	}
}

func TestS3Communicator_ListBuckets(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	reqStr := "GET / HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"

	// 1. No configured bucket -> empty bucket list.
	respStr := runS3TestRequest(t, reqStr, &mockStore{})
	if !strings.Contains(respStr, "HTTP/1.1 200 OK") {
		t.Errorf("Expected 200 OK, got: %s", respStr)
	}
	if !strings.Contains(respStr, "Content-Type: application/xml") {
		t.Errorf("Expected XML content type, got: %s", respStr)
	}
	if !strings.Contains(respStr, "<ListAllMyBucketsResult") {
		t.Errorf("Expected ListAllMyBucketsResult, got: %s", respStr)
	}
	if strings.Contains(respStr, "<Name>") {
		t.Errorf("Expected empty bucket list without configured bucket, got: %s", respStr)
	}

	// 2. Configured bucket -> returned in ListBuckets.
	respStr, serverErr := runS3RequestCaptureBucket(t, reqStr, &mockStore{}, "mybucket")
	if serverErr != ErrRequestHandled {
		t.Fatalf("Expected ErrRequestHandled, got: %v", serverErr)
	}
	if !strings.Contains(respStr, "<Name>mybucket</Name>") {
		t.Errorf("Expected configured bucket in ListBuckets, got: %s", respStr)
	}
}

func TestS3Communicator_GetBucketLocation(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

	// 1. Valid bucket -> 200 with empty LocationConstraint (us-east-1).
	reqStr := "GET /mybucket?location HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"
	respStr, serverErr := runS3RequestCaptureBucket(t, reqStr, &mockStore{}, "mybucket")
	if serverErr != ErrRequestHandled {
		t.Fatalf("Expected ErrRequestHandled, got: %v", serverErr)
	}
	if !strings.Contains(respStr, "HTTP/1.1 200 OK") {
		t.Errorf("Expected 200 OK, got: %s", respStr)
	}
	if !strings.Contains(respStr, "<LocationConstraint") {
		t.Errorf("Expected LocationConstraint XML, got: %s", respStr)
	}

	// 2. Unknown bucket -> 404 NoSuchBucket.
	respStr, serverErr = runS3RequestCaptureBucket(t, reqStr, &mockStore{}, "otherbucket")
	if serverErr == nil || !errors.Is(serverErr, syscall.ENOENT) {
		t.Fatalf("Expected ENOENT for unknown bucket, got: %v", serverErr)
	}
	if !strings.Contains(respStr, "HTTP/1.1 404 Not Found") {
		t.Errorf("Expected 404 Not Found, got: %s", respStr)
	}
	if !strings.Contains(respStr, "<Code>NoSuchBucket</Code>") {
		t.Errorf("Expected NoSuchBucket XML error, got: %s", respStr)
	}
}

func TestS3Communicator_CreateBucket(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

	// 1. CreateBucket for the configured bucket -> 200 + Location header.
	reqStr := "PUT /mybucket HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n" +
		"Content-Length: 0\r\n\r\n"
	respStr, serverErr := runS3RequestCaptureBucket(t, reqStr, &mockStore{}, "mybucket")
	if serverErr != ErrRequestHandled {
		t.Fatalf("Expected ErrRequestHandled for CreateBucket, got: %v", serverErr)
	}
	if !strings.Contains(respStr, "HTTP/1.1 200 OK") {
		t.Errorf("Expected 200 OK, got: %s", respStr)
	}
	if !strings.Contains(respStr, "Location: /mybucket") {
		t.Errorf("Expected Location header, got: %s", respStr)
	}
	if !strings.Contains(respStr, "<LocationConstraint") {
		t.Errorf("Expected LocationConstraint XML, got: %s", respStr)
	}

	// 2. CreateBucket for a different name -> 404 NoSuchBucket (single-bucket policy).
	respStr, serverErr = runS3RequestCaptureBucket(t, reqStr, &mockStore{}, "configured-bucket")
	if serverErr == nil || !errors.Is(serverErr, syscall.ENOENT) {
		t.Fatalf("Expected ENOENT for wrong bucket name, got: %v", serverErr)
	}
	if !strings.Contains(respStr, "HTTP/1.1 404 Not Found") {
		t.Errorf("Expected 404 Not Found, got: %s", respStr)
	}
	if !strings.Contains(respStr, "<Code>NoSuchBucket</Code>") {
		t.Errorf("Expected NoSuchBucket XML error, got: %s", respStr)
	}
}

func TestS3Communicator_DeleteBucket(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	reqStr := "DELETE /mybucket HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"

	// 1. Empty store -> 204 No Content.
	emptyMock := &mockStore{listFunc: func() ([]common.FileMetadata, error) { return nil, nil }}
	respStr, serverErr := runS3RequestCaptureBucket(t, reqStr, emptyMock, "mybucket")
	if serverErr != ErrRequestHandled {
		t.Fatalf("Expected ErrRequestHandled for empty DeleteBucket, got: %v", serverErr)
	}
	if !strings.Contains(respStr, "HTTP/1.1 204 No Content") {
		t.Errorf("Expected 204 No Content, got: %s", respStr)
	}

	// 2. Non-empty store -> 409 BucketNotEmpty.
	nonEmptyMock := &mockStore{listFunc: func() ([]common.FileMetadata, error) {
		return []common.FileMetadata{{Name: "file.txt", Hash: "h", Size: 1}}, nil
	}}
	respStr, serverErr = runS3RequestCaptureBucket(t, reqStr, nonEmptyMock, "mybucket")
	if serverErr == nil || !errors.Is(serverErr, syscall.ENOTEMPTY) {
		t.Fatalf("Expected ENOTEMPTY for non-empty DeleteBucket, got: %v", serverErr)
	}
	if !strings.Contains(respStr, "HTTP/1.1 409 Conflict") {
		t.Errorf("Expected 409 Conflict, got: %s", respStr)
	}
	if !strings.Contains(respStr, "<Code>BucketNotEmpty</Code>") {
		t.Errorf("Expected BucketNotEmpty XML error, got: %s", respStr)
	}

	// 3. Unknown bucket name -> 404 NoSuchBucket.
	respStr, serverErr = runS3RequestCaptureBucket(t, reqStr, emptyMock, "configured-bucket")
	if serverErr == nil || !errors.Is(serverErr, syscall.ENOENT) {
		t.Fatalf("Expected ENOENT for wrong bucket, got: %v", serverErr)
	}
	if !strings.Contains(respStr, "HTTP/1.1 404 Not Found") {
		t.Errorf("Expected 404 Not Found, got: %s", respStr)
	}
	if !strings.Contains(respStr, "<Code>NoSuchBucket</Code>") {
		t.Errorf("Expected NoSuchBucket XML error, got: %s", respStr)
	}

	// 4. Legacy flat mode (no configured bucket) preserves 400 for keyless DELETE.
	respStr, serverErr = runS3RequestCapture(t, reqStr, emptyMock)
	if serverErr == nil {
		t.Fatal("Expected error for keyless DELETE in flat mode")
	}
	if !strings.Contains(respStr, "HTTP/1.1 400 Bad Request") {
		t.Errorf("Expected 400 Bad Request in flat mode, got: %s", respStr)
	}
}

func TestS3Communicator_HeadBucket_Configured(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

	// 1. HEAD configured bucket -> 200.
	reqStr := "HEAD /mybucket HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"
	respStr, serverErr := runS3RequestCaptureBucket(t, reqStr, &mockStore{}, "mybucket")
	if serverErr != ErrRequestHandled {
		t.Fatalf("Expected ErrRequestHandled, got: %v", serverErr)
	}
	if !strings.Contains(respStr, "HTTP/1.1 200 OK") {
		t.Errorf("Expected 200 OK, got: %s", respStr)
	}

	// 2. HEAD unknown bucket -> 404 NoSuchBucket.
	respStr, serverErr = runS3RequestCaptureBucket(t, reqStr, &mockStore{}, "otherbucket")
	if serverErr == nil || !errors.Is(serverErr, syscall.ENOENT) {
		t.Fatalf("Expected ENOENT for unknown bucket, got: %v", serverErr)
	}
	if !strings.Contains(respStr, "HTTP/1.1 404 Not Found") {
		t.Errorf("Expected 404 Not Found, got: %s", respStr)
	}
	if !strings.Contains(respStr, "<Code>NoSuchBucket</Code>") {
		t.Errorf("Expected NoSuchBucket XML error, got: %s", respStr)
	}
}

func TestS3Communicator_BucketModeObjectOps(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	fileContent := []byte("data")

	// 1. GET object in the configured bucket -> 200.
	reqGet := "GET /mybucket/hello.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"
	mock := &mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			return io.NopCloser(bytes.NewReader(fileContent)), common.FileMetadata{Name: name, Hash: "h1", Size: 4}, nil
		},
	}
	respStr, serverErr := runS3RequestCaptureBucket(t, reqGet, mock, "mybucket")
	if serverErr != ErrRequestHandled {
		t.Fatalf("Expected ErrRequestHandled, got: %v", serverErr)
	}
	if !strings.Contains(respStr, "HTTP/1.1 200 OK") {
		t.Errorf("Expected 200 OK, got: %s", respStr)
	}

	// 2. GET object in an unknown bucket -> 404 NoSuchBucket.
	respStr, serverErr = runS3RequestCaptureBucket(t, reqGet, mock, "configured-bucket")
	if serverErr == nil || !errors.Is(serverErr, syscall.ENOENT) {
		t.Fatalf("Expected ENOENT for wrong bucket, got: %v", serverErr)
	}
	if !strings.Contains(respStr, "<Code>NoSuchBucket</Code>") {
		t.Errorf("Expected NoSuchBucket XML error, got: %s", respStr)
	}

	// 3. PUT object in the configured bucket -> handshake proceeds (key stored without bucket prefix).
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))

	putReq := "PUT /mybucket/subdir/file.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n" +
		"X-Amz-Date: 20260604T120000Z\r\n" +
		"X-Amz-Content-Sha256: hash123\r\n" +
		"Content-Length: 1024\r\n\r\n"

	go func() {
		clientConn.Write([]byte(putReq))
		buf := make([]byte, 1024)
		for {
			if _, err := clientConn.Read(buf); err != nil {
				break
			}
		}
	}()

	comm := NewS3Communicator(serverConn)
	comm.SetStore(mock)
	comm.SetConfiguredBucket("mybucket")
	_, _, err := comm.HandshakeServer(expectedAuthToken)
	if err != nil {
		t.Fatalf("HandshakeServer failed for PUT in configured bucket: %v", err)
	}
	meta, err := comm.ReceiveMetadata()
	if err != nil {
		t.Fatalf("ReceiveMetadata failed: %v", err)
	}
	if meta.Name != "subdir/file.txt" {
		t.Errorf("Expected bucket-relative key name, got %q", meta.Name)
	}
}

func TestS3Communicator_HandshakeClient_CRLFInjection(t *testing.T) {
	defer verifyNoLeaks(t)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	comm := NewS3Communicator(serverConn)

	_, err := comm.HandshakeClient("evil\r\nX-Injected: true", 12345, 1)
	if err == nil {
		t.Fatal("Expected error for CRLF in auth token, got nil")
	}
	if !strings.Contains(err.Error(), "CRLF") {
		t.Errorf("Expected CRLF error, got: %v", err)
	}
}

func TestS3Communicator_HandshakeClient_ReadDeadline(t *testing.T) {
	defer verifyNoLeaks(t)

	origTimeout := s3ReadHeaderTimeout
	s3ReadHeaderTimeout = 50 * time.Millisecond
	defer func() { s3ReadHeaderTimeout = origTimeout }()

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	// The peer accepts the handshake request but never writes a response,
	// simulating a malicious/unresponsive server (issue #620).
	go func() {
		buf := make([]byte, 1024)
		for {
			if _, err := clientConn.Read(buf); err != nil {
				return
			}
		}
	}()

	comm := NewS3Communicator(serverConn)
	start := time.Now()
	_, err := comm.HandshakeClient("valid-token", 12345, 1)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected HandshakeClient to fail on unresponsive server, got nil")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("HandshakeClient blocked for %v instead of honoring read deadline", elapsed)
	}
}

func TestS3Communicator_SendMetadata_ReadDeadline(t *testing.T) {
	defer verifyNoLeaks(t)

	origTimeout := s3ReadHeaderTimeout
	s3ReadHeaderTimeout = 50 * time.Millisecond
	defer func() { s3ReadHeaderTimeout = origTimeout }()

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		buf := make([]byte, 1024)
		for {
			if _, err := clientConn.Read(buf); err != nil {
				return
			}
		}
	}()

	comm := NewS3Communicator(serverConn)
	meta := &common.FileMetadata{Name: "test-file.txt", Hash: "hash1", Size: 100}
	start := time.Now()
	_, err := comm.SendMetadata(meta)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected SendMetadata to fail on unresponsive server, got nil")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("SendMetadata blocked for %v instead of honoring read deadline", elapsed)
	}
}

func TestS3Communicator_ReceiveACK_ReadDeadline(t *testing.T) {
	defer verifyNoLeaks(t)

	origTimeout := s3ReadHeaderTimeout
	s3ReadHeaderTimeout = 50 * time.Millisecond
	defer func() { s3ReadHeaderTimeout = origTimeout }()

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	comm := NewS3Communicator(serverConn)
	start := time.Now()
	err := comm.ReceiveACK()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected ReceiveACK to fail on unresponsive server, got nil")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("ReceiveACK blocked for %v instead of honoring read deadline", elapsed)
	}
}

func TestS3Communicator_SendMetadataPathTraversal(t *testing.T) {
	defer verifyNoLeaks(t)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	comm := NewS3Communicator(clientConn)

	// Malicious Name
	badMeta := &common.FileMetadata{
		Name: "../passwd",
		Hash: "hash123",
		Size: 100,
	}
	_, err := comm.SendMetadata(badMeta)
	if err == nil {
		t.Fatal("Expected SendMetadata to fail with path traversal name")
	}
	if !errors.Is(err, syscall.EBADMSG) {
		t.Errorf("Expected EBADMSG error, got: %v", err)
	}
}

func TestWriteS3Error(t *testing.T) {
	var buf bytes.Buffer
	n, err := writeS3Error(&buf, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.", "mybucket/missing.txt")
	if err != nil {
		t.Fatalf("writeS3Error failed: %v", err)
	}

	respStr := buf.String()

	if !strings.HasPrefix(respStr, "HTTP/1.1 404 Not Found\r\n") {
		t.Errorf("Expected 404 status line, got: %s", respStr)
	}
	if !strings.Contains(respStr, "Content-Type: application/xml\r\n") {
		t.Errorf("Expected XML content type, got: %s", respStr)
	}
	if !strings.Contains(respStr, "Connection: close\r\n") {
		t.Errorf("Expected Connection: close header, got: %s", respStr)
	}
	if !strings.Contains(respStr, "<Code>NoSuchKey</Code>") {
		t.Errorf("Expected NoSuchKey code in body, got: %s", respStr)
	}
	if !strings.Contains(respStr, "<Message>The specified key does not exist.</Message>") {
		t.Errorf("Expected message in body, got: %s", respStr)
	}
	if !strings.Contains(respStr, "<Resource>mybucket/missing.txt</Resource>") {
		t.Errorf("Expected resource in body, got: %s", respStr)
	}

	// Content-Length must exactly match the XML body length.
	headerEnd := strings.Index(respStr, "\r\n\r\n")
	if headerEnd == -1 {
		t.Fatalf("No header/body separator in response: %s", respStr)
	}
	bodyLen := len(respStr) - headerEnd - 4
	clRe := regexp.MustCompile(`Content-Length: (\d+)`)
	m := clRe.FindStringSubmatch(respStr)
	if m == nil {
		t.Fatalf("Content-Length header not found: %s", respStr)
	}
	cl, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("Content-Length not an integer: %v", err)
	}
	if cl != bodyLen {
		t.Errorf("Content-Length (%d) must match body length (%d)", cl, bodyLen)
	}

	// n must equal the total bytes written (headers + body).
	if n != len(respStr) {
		t.Errorf("writeS3Error returned %d bytes but wrote %d", n, len(respStr))
	}

	// XML special characters must be escaped in attacker-controlled fields.
	buf.Reset()
	_, _ = writeS3Error(&buf, http.StatusBadRequest, "InvalidArgument", `bad <msg> & "quoted" 'single'`, `a<b&c>d`)
	escaped := buf.String()
	if strings.Contains(escaped, `<Message>bad <msg>`) {
		t.Errorf("Message XML not escaped: %s", escaped)
	}
	if !strings.Contains(escaped, `<Message>bad &lt;msg&gt; &amp; &quot;quoted&quot; &apos;single&apos;</Message>`) {
		t.Errorf("Message not escaped correctly: %s", escaped)
	}
	if !strings.Contains(escaped, `<Resource>a&lt;b&amp;c&gt;d</Resource>`) {
		t.Errorf("Resource not escaped correctly: %s", escaped)
	}

	// CR/LF must be stripped to prevent HTTP response splitting.
	buf.Reset()
	_, _ = writeS3Error(&buf, http.StatusBadRequest, "InvalidArgument", "evil\r\nX-Injected: true\r\n", "")
	stripped := buf.String()
	if strings.Contains(stripped, "evil\r\n") {
		t.Errorf("CR not stripped from message, response splitting possible: %s", stripped)
	}
	if strings.Contains(stripped, "true\r\n") {
		t.Errorf("LF not stripped from message, response splitting possible: %s", stripped)
	}
	if !strings.Contains(stripped, "<Message>evil  X-Injected: true  </Message>") {
		t.Errorf("Expected CR/LF replaced with spaces in message, got: %s", stripped)
	}
	// The response must contain exactly one header/body terminator.
	if strings.Count(stripped, "\r\n\r\n") != 1 {
		t.Errorf("Expected exactly one header/body separator, got: %s", stripped)
	}

	// Oversized messages are truncated to 512 bytes.
	buf.Reset()
	longMsg := strings.Repeat("a", 2048)
	_, _ = writeS3Error(&buf, http.StatusBadRequest, "InvalidArgument", longMsg, "")
	if !strings.Contains(buf.String(), strings.Repeat("a", 512)) {
		t.Errorf("Expected message truncated to 512 bytes, got: %d", len(buf.String()))
	}
	if strings.Contains(buf.String(), strings.Repeat("a", 513)) {
		t.Errorf("Message not truncated to 512 bytes")
	}

	// Resource is omitted when empty.
	buf.Reset()
	_, _ = writeS3Error(&buf, http.StatusForbidden, "AccessDenied", "Access Denied.", "")
	if strings.Contains(buf.String(), "<Resource>") {
		t.Errorf("Empty resource should be omitted, got: %s", buf.String())
	}
}

func TestS3ErrorCodeMapping(t *testing.T) {
	cases := []struct {
		status int
		code   string
	}{
		{http.StatusBadRequest, "InvalidArgument"},
		{http.StatusForbidden, "AccessDenied"},
		{http.StatusNotFound, "NoSuchKey"},
		{http.StatusMethodNotAllowed, "MethodNotAllowed"},
		{http.StatusConflict, "BucketNotEmpty"},
		{http.StatusRequestEntityTooLarge, "EntityTooLarge"},
		{http.StatusUnsupportedMediaType, "InvalidRequest"},
		{http.StatusInternalServerError, "InternalError"},
		{http.StatusServiceUnavailable, "ServiceUnavailable"},
		{http.StatusGatewayTimeout, "InternalError"},
	}
	for _, tc := range cases {
		if got := s3ErrorCode(tc.status); got != tc.code {
			t.Errorf("s3ErrorCode(%d) = %q, want %q", tc.status, got, tc.code)
		}
	}
}

func TestS3Communicator_XMLErrorResponses(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

	validTokenReq := func(method, target string) string {
		return method + " " + target + " HTTP/1.1\r\n" +
			"Host: 127.0.0.1:4440\r\n" +
			"Authorization: Bearer " + authToken + "\r\n\r\n"
	}

	// 1. GET missing key -> 404 NoSuchKey
	mock := &mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			return nil, common.FileMetadata{}, syscall.ENOENT
		},
	}
	resp, serverErr := runS3RequestCapture(t, validTokenReq("GET", "/bucket/missing.txt"), mock)
	if serverErr != ErrRequestHandled {
		t.Fatalf("Expected ErrRequestHandled for missing key, got: %v", serverErr)
	}
	if !strings.Contains(resp, "HTTP/1.1 404 Not Found") {
		t.Errorf("Expected 404 Not Found, got: %s", resp)
	}
	if !strings.Contains(resp, "<Code>NoSuchKey</Code>") {
		t.Errorf("Expected NoSuchKey XML error, got: %s", resp)
	}
	if !strings.Contains(resp, "<Resource>missing.txt</Resource>") {
		t.Errorf("Expected missing.txt resource in error, got: %s", resp)
	}

	// 2. DELETE with no key -> 400 InvalidArgument
	resp, serverErr = runS3RequestCapture(t, validTokenReq("DELETE", "/bucket"), &mockStore{})
	if serverErr == nil {
		t.Fatalf("Expected error for DELETE without key, got: %v", serverErr)
	}
	if !strings.Contains(resp, "HTTP/1.1 400 Bad Request") {
		t.Errorf("Expected 400 Bad Request, got: %s", resp)
	}
	if !strings.Contains(resp, "<Code>InvalidArgument</Code>") {
		t.Errorf("Expected InvalidArgument XML error, got: %s", resp)
	}

	// 3. Storage store not initialized -> 500 InternalError
	resp, serverErr = runS3RequestCapture(t, validTokenReq("GET", "/bucket/file.txt"), nil)
	if serverErr == nil {
		t.Fatal("Expected error for nil store")
	}
	if !strings.Contains(resp, "HTTP/1.1 500 Internal Server Error") {
		t.Errorf("Expected 500 Internal Server Error, got: %s", resp)
	}
	if !strings.Contains(resp, "<Code>InternalError</Code>") {
		t.Errorf("Expected InternalError XML error, got: %s", resp)
	}

	// 4. Path traversal -> 400 InvalidArgument
	resp, serverErr = runS3RequestCapture(t, validTokenReq("GET", "/bucket/../../etc/passwd"), &mockStore{})
	if serverErr == nil || !errors.Is(serverErr, syscall.EBADMSG) {
		t.Fatalf("Expected EBADMSG for path traversal, got: %v", serverErr)
	}
	if !strings.Contains(resp, "HTTP/1.1 400 Bad Request") {
		t.Errorf("Expected 400 Bad Request, got: %s", resp)
	}
	if !strings.Contains(resp, "<Code>InvalidArgument</Code>") {
		t.Errorf("Expected InvalidArgument XML error, got: %s", resp)
	}
}

func TestS3Communicator_XMLErrorAuthFailures(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

	// 1. Wrong bearer token -> 403 AccessDenied
	wrongTokenReq := "PUT /bucket/file.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer wrong-token\r\n" +
		"Content-Length: 0\r\n\r\n"

	resp, serverErr := runS3RequestCapture(t, wrongTokenReq, &mockStore{})
	if serverErr == nil || !errors.Is(serverErr, syscall.EACCES) {
		t.Fatalf("Expected EACCES for wrong token, got: %v", serverErr)
	}
	if !strings.Contains(resp, "HTTP/1.1 403 Forbidden") {
		t.Errorf("Expected 403 Forbidden, got: %s", resp)
	}
	if !strings.Contains(resp, "<Code>AccessDenied</Code>") {
		t.Errorf("Expected AccessDenied XML error, got: %s", resp)
	}

	// 2. Malformed SigV4 header -> 403 AuthorizationHeaderMalformed
	malformedAuthReq := "PUT /bucket/file.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: AWS4-HMAC-SHA256 this-is-not-a-valid-header\r\n" +
		"X-Amz-Date: 20260604T120000Z\r\n" +
		"Content-Length: 0\r\n\r\n"

	resp, serverErr = runS3RequestCapture(t, malformedAuthReq, &mockStore{})
	if serverErr == nil || !errors.Is(serverErr, syscall.EACCES) {
		t.Fatalf("Expected EACCES for malformed SigV4, got: %v", serverErr)
	}
	if !strings.Contains(resp, "HTTP/1.1 403 Forbidden") {
		t.Errorf("Expected 403 Forbidden, got: %s", resp)
	}
	if !strings.Contains(resp, "<Code>AuthorizationHeaderMalformed</Code>") {
		t.Errorf("Expected AuthorizationHeaderMalformed XML error, got: %s", resp)
	}

	// 3. SigV4 with correct credentials but wrong signature -> 403 SignatureDoesNotMatch
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := "us-east-1"
	payloadHash := "dummyhash"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalRequest := "PUT\n/bucket/file.txt\n\nhost:127.0.0.1:4440\nx-amz-content-sha256:" + payloadHash + "\nx-amz-date:" + amzDate + "\n\n" + signedHeaders + "\n" + payloadHash
	stringToSign := buildStringToSign(canonicalRequest, amzDate, dateStamp, region)
	signingKey := deriveSigningKey(authToken, dateStamp, region)
	goodSignature := computeSignature(signingKey, stringToSign)
	badSignature := "0000" + goodSignature[4:]

	authHeader := "AWS4-HMAC-SHA256 Credential=" + authToken + "/" + dateStamp + "/" + region + "/s3/aws4_request, SignedHeaders=" + signedHeaders + ", Signature=" + badSignature

	badSigReq := "PUT /bucket/file.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: " + authHeader + "\r\n" +
		"X-Amz-Date: " + amzDate + "\r\n" +
		"X-Amz-Content-Sha256: " + payloadHash + "\r\n" +
		"Content-Length: 0\r\n\r\n"

	resp, serverErr = runS3RequestCapture(t, badSigReq, &mockStore{})
	if serverErr == nil || !errors.Is(serverErr, syscall.EACCES) {
		t.Fatalf("Expected EACCES for bad SigV4 signature, got: %v", serverErr)
	}
	if !strings.Contains(resp, "HTTP/1.1 403 Forbidden") {
		t.Errorf("Expected 403 Forbidden, got: %s", resp)
	}
	if !strings.Contains(resp, "<Code>SignatureDoesNotMatch</Code>") {
		t.Errorf("Expected SignatureDoesNotMatch XML error, got: %s", resp)
	}

	// 4. All error bodies must be well-formed XML (parse each captured response).
	for _, r := range []string{
		runCaptureReq(t, wrongTokenReq, &mockStore{}),
		runCaptureReq(t, malformedAuthReq, &mockStore{}),
		runCaptureReq(t, badSigReq, &mockStore{}),
	} {
		headerEnd := strings.Index(r, "\r\n\r\n")
		if headerEnd == -1 {
			t.Fatalf("Malformed HTTP response (no header terminator): %s", r)
		}
		body := r[headerEnd+4:]
		if !strings.Contains(body, "<Error>") || !strings.Contains(body, "</Error>") {
			t.Errorf("Error body is not an <Error> XML document: %s", body)
		}
	}
}

func runCaptureReq(t *testing.T, reqStr string, mock storage.Store) string {
	resp, _ := runS3RequestCapture(t, reqStr, mock)
	return resp
}
