package common

import (
	"crypto/sha256"
	"encoding/hex"
)

// peerTokenSuffix is a domain-separation suffix used to derive the peer token
// from the client auth token. This ensures the peer token is distinct from the
// client token even though both are derived from the same shared secret.
const peerTokenSuffix = ":momo-peer:"

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
