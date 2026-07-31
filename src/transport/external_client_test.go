package transport

import (
	"net"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

func TestS3Communicator_ExternalClientDetection(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))

	t.Run("aws-cli without X-Momo-Requested-Mode is external", func(t *testing.T) {
		amzDate := "20260604T120000Z"
		dateStamp := "20260604"
		region := "us-east-1"
		payloadHash := "dummyhash"
		signedHeaders := "host;x-amz-date"

		canonicalRequest := "PUT\n/test-file.txt\n\nhost:127.0.0.1:4440\nx-amz-date:" + amzDate + "\n\n" + signedHeaders + "\n" + payloadHash
		stringToSign := buildStringToSign(canonicalRequest, amzDate, dateStamp, region)
		signingKey := deriveSigningKey(authToken, dateStamp, region)
		signature := computeSignature(signingKey, stringToSign)

		reqBody := "PUT /test-file.txt HTTP/1.1\r\n" +
			"Host: 127.0.0.1:4440\r\n" +
			"Authorization: AWS4-HMAC-SHA256 Credential=" + authToken + "/" + dateStamp + "/" + region + "/s3/aws4_request, SignedHeaders=" + signedHeaders + ", Signature=" + signature + "\r\n" +
			"X-Amz-Date: " + amzDate + "\r\n" +
			"X-Amz-Content-Sha256: " + payloadHash + "\r\n" +
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
		_, timestamp, err := comm.HandshakeServer(expectedAuthToken)
		if err != nil {
			t.Fatalf("HandshakeServer failed: %v", err)
		}

		if !comm.IsExternalClient() {
			t.Error("Expected IsExternalClient() to be true for aws-cli without X-Momo-Requested-Mode")
		}

		if timestamp != common.DummyEpoch {
			t.Errorf("Expected timestamp = DummyEpoch (%d), got %d", common.DummyEpoch, timestamp)
		}
	})

	t.Run("momo peer with X-Momo-Requested-Mode is not external", func(t *testing.T) {
		reqBody := "PUT /test-file.txt HTTP/1.1\r\n" +
			"Host: 127.0.0.1:4440\r\n" +
			"Authorization: Bearer " + authToken + "\r\n" +
			"X-Momo-Timestamp: 1750000000000000000\r\n" +
			"X-Momo-Requested-Mode: 2\r\n" +
			"X-Amz-Content-Sha256: dummyhash\r\n" +
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
		requestedMode, timestamp, err := comm.HandshakeServer(expectedAuthToken)
		if err != nil {
			t.Fatalf("HandshakeServer failed: %v", err)
		}

		if comm.IsExternalClient() {
			t.Error("Expected IsExternalClient() to be false for momo peer with X-Momo-Requested-Mode")
		}

		if requestedMode != 2 {
			t.Errorf("Expected requestedMode = 2, got %d", requestedMode)
		}

		if timestamp == common.DummyEpoch {
			t.Error("Expected timestamp to be the parsed value, not DummyEpoch")
		}
	})
}

func TestMomoTCPCommunicator_IsExternalClient(t *testing.T) {
	defer goleak.VerifyNone(t)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	comm := NewMomoTCPCommunicator(serverConn)
	if comm.IsExternalClient() {
		t.Error("MomoTCPCommunicator should never be external client")
	}
}
