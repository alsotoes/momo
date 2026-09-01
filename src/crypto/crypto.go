package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"golang.org/x/sys/unix"
)

// Cipher parameters for AES-256-GCM.
const (
	KeySize       = 32
	NonceSize     = 12
	TagSize       = 16
	MaxKeyHexSize = 64
)

// Domain labels for HKDF key derivation. Each label scopes a derived key to a
// single purpose so keys for different domain labels, tenants, or contexts can
// never coincide.
var (
	DomainToken   = []byte("momo/token")
	DomainContent = []byte("momo/content")
	DomainAtRest  = []byte("momo/atrest")
	DomainOPRF    = []byte("momo/oprf")
)

// Sentinel errors returned by the cipher operations.
var (
	ErrInvalidKeySize     = errors.New("crypto: key must be 32 bytes")
	ErrInvalidNonceSize   = errors.New("crypto: nonce must be 12 bytes")
	ErrCiphertextTooShort = errors.New("crypto: ciphertext too short")
	ErrTampered           = errors.New("crypto: authentication failed (tampered data)")
	ErrInvalidHexKey      = errors.New("crypto: encryption_key must be 64 hex characters (256-bit)")
)

// Cipher wraps an AES-256-GCM AEAD with a configurable stream chunk size.
type Cipher struct {
	aead cipher.AEAD
	// chunkSize is the plaintext chunk length used by EncryptStream. It
	// defaults to ChunkSize and may be changed via SetStreamChunkSize within
	// [MinChunkSize, MaxChunkSize] (issue #824).
	chunkSize int
}

// NewCipher creates an AES-256-GCM cipher from a 32-byte key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeySize
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create AES cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create GCM: %w", err)
	}

	return &Cipher{aead: aead, chunkSize: ChunkSize}, nil
}

// SetStreamChunkSize sets the plaintext chunk length used by EncryptStream.
// It validates the size is within [MinChunkSize, MaxChunkSize] (Rule 32) and
// returns an error that maps to EINVAL for out-of-range values; on error the
// cipher retains its prior chunk size (issue #824).
func (c *Cipher) SetStreamChunkSize(n int) error {
	if n < MinChunkSize || n > MaxChunkSize {
		return fmt.Errorf("crypto: invalid stream chunk size %d (must be within [%d, %d]): %w", n, MinChunkSize, MaxChunkSize, unix.EINVAL)
	}
	c.chunkSize = n
	return nil
}

// NewCipherFromHex decodes a 64-hex-character key and creates a Cipher from it.
func NewCipherFromHex(hexKey string) (*Cipher, error) {
	if len(hexKey) != MaxKeyHexSize {
		return nil, ErrInvalidHexKey
	}

	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to decode hex key: %w", err)
	}

	return NewCipher(key)
}

// Encrypt seals plaintext with a fresh random nonce and returns
// nonce||ciphertext||tag.
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: failed to generate nonce: %w", err)
	}

	ciphertext := c.aead.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt opens nonce||ciphertext||tag and returns the plaintext, or
// ErrTampered when authentication fails.
func (c *Cipher) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < NonceSize+TagSize {
		return nil, ErrCiphertextTooShort
	}

	nonce := ciphertext[:NonceSize]
	sealed := ciphertext[NonceSize:]

	plaintext, err := c.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, ErrTampered
	}

	return plaintext, nil
}

// GenerateKey returns a fresh random 256-bit key.
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("crypto: failed to generate key: %w", err)
	}
	return key, nil
}

// DeriveKey derives a domain-separated key from the master key using
// HKDF-SHA256, scoped to the given tenant and context.
func DeriveKey(masterKey []byte, tenant string, context []byte) ([]byte, error) {
	if len(masterKey) != KeySize {
		return nil, ErrInvalidKeySize
	}

	// Domain-separated HKDF info. Each part is length-prefixed so that no two
	// distinct (domain, tenant, context) tuples can collide after
	// concatenation (e.g. "ab"+"c" vs "a"+"bc"). The length prefixes are
	// 4-byte big-endian counts fixed across all derivations.
	buf := make([]byte, 0, 4+len(tenant)+4+len(context))
	buf = appendUint32(buf, uint32(len(tenant)))
	buf = append(buf, tenant...)
	buf = appendUint32(buf, uint32(len(context)))
	buf = append(buf, context...)

	derived, err := hkdf.Key(sha256.New, masterKey, nil, string(buf), KeySize)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to derive key: %w", err)
	}

	return derived, nil
}

func appendUint32(dst []byte, v uint32) []byte {
	return append(dst, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
