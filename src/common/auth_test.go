package common

import (
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
