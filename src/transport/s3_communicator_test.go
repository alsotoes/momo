package transport

import (
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"

	"github.com/alsotoes/momo/src/common"
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

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
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

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
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

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
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
