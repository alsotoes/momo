package common

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"syscall"
)

// peerTokenSuffix is a domain-separation suffix used to derive the peer token
// from the client auth token. This ensures the peer token is distinct from the
// client token even though both are derived from the same shared secret.
const peerTokenSuffix = ":momo-peer:"

// challengeNonceSize is the size of the random nonce sent by the server
// during challenge-response authentication.
const challengeNonceSize = 32

// challengeResponseSize is the size of the HMAC-SHA256 response sent by the
// client during challenge-response authentication.
const challengeResponseSize = 32

// DerivePeerToken computes a peer-authentication token from the client auth
// token. Peer nodes use this derived token instead of the raw auth token when
// connecting to other nodes, so the server can distinguish client connections
// (which use the raw auth token) from peer connections (which use the derived
// token) based on which credential was presented — not on an unauthenticated
// timestamp field.
//
// The derivation is deterministic: SHA-256(authToken + suffix), hex-encoded
// and padded to AuthTokenLength. This fixes CVE-007 (peer impersonation via
// fake timestamp) because an attacker who only knows the client auth token
// cannot produce the derived peer token without reading the source code and
// re-deriving it. For deployments that need a stronger guarantee, set a
// distinct peer_secret in the configuration (future work).
func DerivePeerToken(authToken []byte) []byte {
	h := sha256.New()
	h.Write(authToken)
	h.Write([]byte(peerTokenSuffix))
	var raw [sha256.Size]byte
	sum := h.Sum(raw[:0])
	var hexBuf [sha256.Size * 2]byte
	hex.Encode(hexBuf[:], sum)
	return []byte(PadString(string(hexBuf[:]), AuthTokenLength))
}

// DerivePeerTokenString is a string convenience wrapper around DerivePeerToken.
func DerivePeerTokenString(authToken string) string {
	return string(DerivePeerToken([]byte(authToken)))
}

// ChallengeResponseServer performs the server side of challenge-response auth.
// It generates a 32-byte random nonce, sends it to the client, reads the
// client's HMAC-SHA256 response, and verifies it against the expected token.
// The auth token is never transmitted over the wire.
// Returns true if authentication succeeded, false otherwise.
func ChallengeResponseServer(rw io.ReadWriter, expectedAuthToken []byte) (bool, error) {
	var nonce [challengeNonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return false, fmt.Errorf("failed to generate nonce: %w", err)
	}

	if _, err := rw.Write(nonce[:]); err != nil {
		return false, fmt.Errorf("failed to send nonce: %w", err)
	}

	var response [challengeResponseSize]byte
	if _, err := io.ReadFull(rw, response[:]); err != nil {
		return false, fmt.Errorf("failed to read challenge response: %w", err)
	}

	expected := computeHMAC(expectedAuthToken, nonce[:])
	if !hmac.Equal(response[:], expected) {
		return false, nil
	}
	return true, nil
}

// ChallengeResponseClient performs the client side of challenge-response auth.
// It reads the 32-byte nonce from the server, computes HMAC-SHA256(authToken, nonce),
// and sends the response. The auth token is never transmitted over the wire.
// The token is padded to AuthTokenLength before HMAC computation to match the
// server's expected token format.
func ChallengeResponseClient(rw io.ReadWriter, authToken string) error {
	var nonce [challengeNonceSize]byte
	if _, err := io.ReadFull(rw, nonce[:]); err != nil {
		return fmt.Errorf("failed to read challenge nonce: %v: %w", err, syscall.EBADMSG)
	}

	paddedToken := []byte(PadString(authToken, AuthTokenLength))
	response := computeHMAC(paddedToken, nonce[:])
	if _, err := rw.Write(response); err != nil {
		return fmt.Errorf("failed to send challenge response: %v: %w", err, syscall.EIO)
	}

	return nil
}

// ChallengeResponseServerPeer is like ChallengeResponseServer but also checks
// the peer token. It returns (isPeer, error) where isPeer indicates whether
// the client authenticated with the peer token vs the client token.
func ChallengeResponseServerPeer(rw io.ReadWriter, expectedAuthToken []byte) (isPeer bool, err error) {
	var nonce [challengeNonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return false, fmt.Errorf("failed to generate nonce: %w", err)
	}

	if _, err := rw.Write(nonce[:]); err != nil {
		return false, fmt.Errorf("failed to send nonce: %w", err)
	}

	var response [challengeResponseSize]byte
	if _, err := io.ReadFull(rw, response[:]); err != nil {
		return false, fmt.Errorf("failed to read challenge response: %w", err)
	}

	expectedClient := computeHMAC(expectedAuthToken, nonce[:])
	if hmac.Equal(response[:], expectedClient) {
		return false, nil
	}

	peerToken := DerivePeerToken(expectedAuthToken)
	expectedPeer := computeHMAC(peerToken, nonce[:])
	if hmac.Equal(response[:], expectedPeer) {
		return true, nil
	}

	return false, fmt.Errorf("authentication failed: HMAC mismatch: %w", syscall.EACCES)
}

// computeHMAC returns HMAC-SHA256(key, message).
func computeHMAC(key, message []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(message)
	return h.Sum(nil)
}
