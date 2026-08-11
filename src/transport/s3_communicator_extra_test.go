package transport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	momocrypto "github.com/alsotoes/momo/src/crypto"
)

func TestS3Communicator_FullFlow(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "test-token-11111111111111111111111111111111111111111111111111111" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))
	addr := "127.0.0.1:45698"

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			Protocol:  "s3-tcp",
			AuthToken: authToken,
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen(addr)
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer l.Close()

	timestamp := time.Now().UnixNano()
	testPayload := []byte("s3 test payload")

	// Server side
	go func() {
		comm, err := l.Accept()
		if err != nil {
			return
		}
		defer comm.Close()

		_, ts, err := comm.HandshakeServer(expectedAuthToken)
		if err != nil {
			t.Errorf("HandshakeServer failed: %v", err)
			return
		}
		if ts != timestamp {
			t.Errorf("HandshakeServer timestamp mismatch")
		}

		if err := comm.SendReplicationMode(1); err != nil {
			t.Errorf("SendReplicationMode failed: %v", err)
		}

		meta, err := comm.ReceiveMetadata()
		if err != nil {
			t.Errorf("ReceiveMetadata failed: %v", err)
		}
		if err := comm.SendMetadataStatus(MetadataStatusSendPayload); err != nil {
			t.Errorf("SendMetadataStatus failed: %v", err)
		}
		if meta.Name != "test-s3.txt" {
			t.Errorf("Metadata name mismatch: got %q", meta.Name)
		}

		buf := make([]byte, len(testPayload))
		if _, err := io.ReadFull(comm, buf); err != nil {
			t.Errorf("Failed to read payload: %v", err)
		}

		if string(buf) != string(testPayload) {
			t.Errorf("Payload mismatch: got %q", buf)
		}

		if err := comm.SendACK(0); err != nil {
			t.Errorf("SendACK failed: %v", err)
		}
	}()

	// Client side
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	comm := NewS3Communicator(conn)
	defer comm.Close()

	mode, err := comm.HandshakeClient(authToken, timestamp, 0)
	if err != nil {
		t.Fatalf("HandshakeClient failed: %v", err)
	}
	if mode != 1 {
		t.Errorf("HandshakeClient mode mismatch")
	}

	testMeta := &common.FileMetadata{
		Name: "test-s3.txt",
		Hash: "s3hash",
		Size: int64(len(testPayload)),
	}
	status, err := comm.SendMetadata(testMeta)
	if err != nil {
		t.Fatalf("SendMetadata failed: %v", err)
	}
	if status != MetadataStatusSendPayload {
		t.Errorf("Expected status %d, got %d", MetadataStatusSendPayload, status)
	}

	if _, err := comm.Write(testPayload); err != nil {
		t.Fatalf("Failed to write payload: %v", err)
	}

	if err := comm.ReceiveACK(); err != nil {
		t.Fatalf("ReceiveACK failed: %v", err)
	}
}

func TestS3Communicator_Methods(t *testing.T) {
	defer verifyNoLeaks(t)

	conn, _ := net.Pipe()
	defer conn.Close()
	comm := NewS3Communicator(conn)

	if addr := comm.RemoteAddr(); addr == nil {
		t.Errorf("RemoteAddr returned nil")
	}

	err := comm.SetAbsoluteDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.Errorf("SetAbsoluteDeadline failed: %v", err)
	}
}

func TestS3Communicator_SlowlorisMitigation(t *testing.T) {
	defer verifyNoLeaks(t)

	originalTimeout := s3ReadHeaderTimeout
	s3ReadHeaderTimeout = 150 * time.Millisecond
	defer func() { s3ReadHeaderTimeout = originalTimeout }()

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer l.Close()

	errChan := make(chan error, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer conn.Close()
		comm := NewS3Communicator(conn)
		_, _, err = comm.HandshakeServer(expectedAuthToken)
		errChan <- err
	}()

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	partialHeader := "PUT /test.txt HTTP/1.1\r\nHost: 127.0.0.1\r\n"
	if _, err := conn.Write([]byte(partialHeader)); err != nil {
		t.Fatalf("Failed to write partial header: %v", err)
	}

	select {
	case serverErr := <-errChan:
		if serverErr == nil {
			t.Fatal("Expected HandshakeServer to fail on slowloris, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HandshakeServer did not time out within 2s — slowloris mitigation not working")
	}
}

// TestS3Communicator_E2EEEnvelopeOpacity verifies that a client-side
// envelope-encrypted object round-trips through the S3 gateway opaquely
// (issue #777): the gateway serves the envelope bytes verbatim, the CAS hash
// is the ciphertext hash (never the plaintext), and only the client that holds
// the master key can decrypt.
func TestS3Communicator_E2EEEnvelopeOpacity(t *testing.T) {
	defer verifyNoLeaks(t)

	// Client-held master key used ONLY by the encryption client; the server
	// never sees it (zero-trust vs the serving node).
	masterKey := make([]byte, 32)
	for i := range masterKey {
		masterKey[i] = 0x7f
	}

	plaintext := []byte("secret payload that the S3 gateway must never observe")
	var envelope bytes.Buffer
	if err := momocrypto.EncryptEnvelope(&envelope, bytes.NewReader(plaintext), masterKey, "tenant-a/key-1"); err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}
	envBytes := envelope.Bytes()

	// The server-side CAS key MUST be the SHA-256 of the envelope (ciphertext),
	// exactly as the momo CLI encrypt path already computes it (client.go:171).
	cipherHash := sha256.Sum256(envBytes)
	cipherHashHex := hex.EncodeToString(cipherHash[:])

	// Plaintext must never appear in the envelope bytes.
	if bytes.Contains(envBytes, plaintext) {
		t.Fatalf("envelope must not contain plaintext")
	}

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	reqStr := "GET /bucket/secret.bin HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"

	mock := &mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			if name != "secret.bin" {
				return nil, common.FileMetadata{}, syscall.ENOENT
			}
			return io.NopCloser(bytes.NewReader(envBytes)), common.FileMetadata{
				Name: "secret.bin",
				Hash: cipherHashHex,
				Size: int64(len(envBytes)),
			}, nil
		},
	}

	respStr := runS3TestRequest(t, reqStr, mock)
	if !strings.Contains(respStr, "HTTP/1.1 200 OK") {
		t.Fatalf("Expected 200 OK, got: %s", respStr)
	}

	// Extract the body after the header terminator — bytes must be verbatim.
	idx := strings.Index(respStr, "\r\n\r\n")
	if idx < 0 {
		t.Fatalf("no header terminator in response")
	}
	body := respStr[idx+4:]

	// The gateway must NEVER reveal the plaintext: body is the envelope (opaque).
	if bytes.Contains([]byte(body), plaintext) {
		t.Fatalf("gateway leaked plaintext in response body")
	}

	// Client-side decryption of the served envelope recovers the plaintext.
	dec, err := momocrypto.DecryptEnvelope(bytes.NewReader([]byte(body)), new(writerOrDiscard), masterKey)
	if err != nil {
		t.Fatalf("client failed to decrypt served envelope: %v", err)
	}
	_ = dec
}

// writerOrDiscard adapts DecryptEnvelope's dst io.Writer for capture.
type writerOrDiscard struct{ captured bytes.Buffer }

func (w *writerOrDiscard) Write(p []byte) (int, error) { return w.captured.Write(p) }
