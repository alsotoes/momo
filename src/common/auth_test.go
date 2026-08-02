package common

import (
	"bytes"
	"io"
	"net"
	"testing"

	"go.uber.org/goleak"
)

func TestDerivePeerToken(t *testing.T) {
	defer goleak.VerifyNone(t)

	authToken := []byte(PadString("a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6", AuthTokenLength)) // notsecret

	peerToken := DerivePeerToken(authToken)

	if len(peerToken) != AuthTokenLength {
		t.Fatalf("Expected peer token length %d, got %d", AuthTokenLength, len(peerToken))
	}

	if string(peerToken) == string(authToken) {
		t.Error("Peer token should differ from auth token")
	}

	peerToken2 := DerivePeerToken(authToken)
	if string(peerToken) != string(peerToken2) {
		t.Error("DerivePeerToken should be deterministic")
	}

	differentAuth := []byte(PadString("different-token-value-here", AuthTokenLength)) // notsecret
	differentPeer := DerivePeerToken(differentAuth)
	if string(peerToken) == string(differentPeer) {
		t.Error("Different auth tokens should produce different peer tokens")
	}
}

func TestChallengeResponse_Success(t *testing.T) {
	defer goleak.VerifyNone(t)

	authToken := "my-secret-token" // notsecret
	expectedAuthToken := []byte(PadString(authToken, AuthTokenLength))

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	errChan := make(chan error, 1)
	go func() {
		isPeer, err := ChallengeResponseServerPeer(server, expectedAuthToken)
		if err != nil {
			errChan <- err
			return
		}
		if isPeer {
			errChan <- io.EOF
			return
		}
		errChan <- nil
	}()

	err := ChallengeResponseClient(client, authToken)
	if err != nil {
		t.Fatalf("Client challenge-response failed: %v", err)
	}

	if err := <-errChan; err != nil {
		t.Fatalf("Server challenge-response failed: %v", err)
	}
}

func TestChallengeResponse_PeerToken(t *testing.T) {
	defer goleak.VerifyNone(t)

	authToken := "my-secret-token" // notsecret
	expectedAuthToken := []byte(PadString(authToken, AuthTokenLength))
	peerToken := string(DerivePeerToken(expectedAuthToken))

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	errChan := make(chan error, 1)
	go func() {
		isPeer, err := ChallengeResponseServerPeer(server, expectedAuthToken)
		if err != nil {
			errChan <- err
			return
		}
		if !isPeer {
			errChan <- io.EOF
			return
		}
		errChan <- nil
	}()

	err := ChallengeResponseClient(client, peerToken)
	if err != nil {
		t.Fatalf("Client challenge-response failed: %v", err)
	}

	if err := <-errChan; err != nil {
		t.Fatalf("Server should recognize peer token: %v", err)
	}
}

func TestChallengeResponse_WrongToken(t *testing.T) {
	defer goleak.VerifyNone(t)

	authToken := "my-secret-token"     // notsecret
	wrongToken := "wrong-secret-token" // notsecret
	expectedAuthToken := []byte(PadString(authToken, AuthTokenLength))

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	errChan := make(chan error, 1)
	go func() {
		_, err := ChallengeResponseServerPeer(server, expectedAuthToken)
		errChan <- err
	}()

	_ = ChallengeResponseClient(client, wrongToken)

	err := <-errChan
	if err == nil {
		t.Fatal("Server should reject wrong token")
	}
}

func TestChallengeResponse_NonceReplayProtection(t *testing.T) {
	defer goleak.VerifyNone(t)

	authToken := "my-secret-token" // notsecret
	expectedAuthToken := []byte(PadString(authToken, AuthTokenLength))

	server1, client1 := net.Pipe()
	defer server1.Close()
	defer client1.Close()

	errChan := make(chan error, 1)
	go func() {
		_, err := ChallengeResponseServerPeer(server1, expectedAuthToken)
		errChan <- err
	}()

	_ = ChallengeResponseClient(client1, authToken)
	if err := <-errChan; err != nil {
		t.Fatalf("First handshake should succeed: %v", err)
	}

	server2, client2 := net.Pipe()
	defer server2.Close()
	defer client2.Close()

	errChan2 := make(chan error, 1)
	go func() {
		_, err := ChallengeResponseServerPeer(server2, expectedAuthToken)
		errChan2 <- err
	}()

	_ = ChallengeResponseClient(client2, authToken)
	if err := <-errChan2; err != nil {
		t.Fatalf("Second handshake with new nonce should succeed: %v", err)
	}
}

func TestComputeHMAC(t *testing.T) {
	defer goleak.VerifyNone(t)

	key := []byte("test-key")     // notsecret
	msg := []byte("test-message") // notsecret

	h1 := computeHMAC(key, msg)
	h2 := computeHMAC(key, msg)

	if !bytes.Equal(h1, h2) {
		t.Error("HMAC should be deterministic for same key and message")
	}

	if len(h1) != 32 {
		t.Fatalf("HMAC-SHA256 should be 32 bytes, got %d", len(h1))
	}

	differentKey := []byte("different-key") // notsecret
	h3 := computeHMAC(differentKey, msg)
	if bytes.Equal(h1, h3) {
		t.Error("Different keys should produce different HMACs")
	}
}
