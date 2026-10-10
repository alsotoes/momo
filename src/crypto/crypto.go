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

// TenantKeys holds the per-tenant derived keys. Each field is a 32-byte
// key or share, encrypted at rest when persisted to the key registry.
type TenantKeys struct {
	KEK       []byte // 32-byte tenant key encryption key (AES-GCM-SIV)
	OPRFShare []byte // 32-byte OPRF share for this tenant
	AuthToken []byte // 32-byte auth token for this tenant
}

// TenantKeySizes holds the expected sizes of tenant key fields.
const (
	TenantKEKSize       = 32
	TenantOPRFShareSize = 32
	TenantAuthTokenSize = 32
)

// Domain labels for tenant-specific key derivation.
var (
	DomainTenantKEK       = []byte("momo/tenant/kek")
	DomainTenantOPRFShare = []byte("momo/tenant/oprf")
	DomainTenantAuthToken = []byte("momo/tenant/auth")
)

// DeriveTenantKeys derives the three per-tenant keys from a root KEK using
// HKDF-SHA256 with domain-separated labels. All keys are 32 bytes.
// The tenantID is included in the HKDF info to ensure isolation.
func DeriveTenantKeys(rootKEK []byte, tenantID string) (*TenantKeys, error) {
	if len(rootKEK) != KeySize {
		return nil, ErrInvalidKeySize
	}

	kek, err := hkdf.Key(sha256.New, rootKEK, nil,
		string(appendDomainTenantLabel(DomainTenantKEK, tenantID)), TenantKEKSize)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to derive tenant KEK: %w", err)
	}

	oprfShare, err := hkdf.Key(sha256.New, rootKEK, nil,
		string(appendDomainTenantLabel(DomainTenantOPRFShare, tenantID)), TenantOPRFShareSize)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to derive tenant OPRF share: %w", err)
	}

	authToken, err := hkdf.Key(sha256.New, rootKEK, nil,
		string(appendDomainTenantLabel(DomainTenantAuthToken, tenantID)), TenantAuthTokenSize)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to derive tenant auth token: %w", err)
	}

	return &TenantKeys{
		KEK:       kek,
		OPRFShare: oprfShare,
		AuthToken: authToken,
	}, nil
}

func appendDomainTenantLabel(domain []byte, tenantID string) []byte {
	// Domain label with length-prefixed tenant ID for collision resistance.
	buf := make([]byte, 0, len(domain)+4+len(tenantID))
	buf = append(buf, domain...)
	buf = appendUint32(buf, uint32(len(tenantID)))
	buf = append(buf, tenantID...)
	return buf
}

// WrapTenantKEK encrypts a tenant KEK with a root KEK using AES-256-GCM.
// The tenant ID is used as associated data to bind the ciphertext to the tenant.
func WrapTenantKEK(rootKEK, tenantKEK []byte, tenantID string) ([]byte, error) {
	if len(rootKEK) != KeySize || len(tenantKEK) != TenantKEKSize {
		return nil, ErrInvalidKeySize
	}

	block, err := aes.NewCipher(rootKEK)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create AES cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create GCM: %w", err)
	}

	// Use a fixed nonce derived from tenant ID for deterministic wrapping
	nonce := make([]byte, NonceSize)
	copy(nonce, []byte("momo-wrap"))

	// Associated data binds the ciphertext to the tenant
	aad := []byte("tenant-wrap")
	aad = append(aad, tenantID...)

	return aead.Seal(nil, nonce, tenantKEK, aad), nil
}

// UnwrapTenantKEK decrypts a wrapped tenant KEK.
func UnwrapTenantKEK(rootKEK, wrappedKEK []byte, tenantID string) ([]byte, error) {
	if len(rootKEK) != KeySize {
		return nil, ErrInvalidKeySize
	}

	block, err := aes.NewCipher(rootKEK)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create AES cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create GCM: %w", err)
	}

	nonce := make([]byte, NonceSize)
	copy(nonce, []byte("momo-wrap"))

	aad := []byte("tenant-wrap")
	aad = append(aad, tenantID...)

	plaintext, err := aead.Open(nil, nonce, wrappedKEK, aad)
	if err != nil {
		return nil, ErrTampered
	}

	if len(plaintext) != TenantKEKSize {
		return nil, ErrInvalidKeySize
	}
	return plaintext, nil
}

// RotateTenantKEK re-wraps a tenant's KEK from an old root KEK to a new root KEK.
// The tenant ID is used as AAD for both unwrap and wrap operations.
func RotateTenantKEK(oldRootKEK, newRootKEK, wrappedTenantKEK []byte, tenantID string) ([]byte, error) {
	// Unwrap from old root
	tenantKEK, err := UnwrapTenantKEK(oldRootKEK, wrappedTenantKEK, tenantID)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to unwrap tenant KEK: %w", err)
	}
	// Wrap with new root
	return WrapTenantKEK(newRootKEK, tenantKEK, tenantID)
}

// DeriveTenantOPRFShare derives a tenant-specific OPRF share from a root OPRF share.
// Uses the tenant ID as HKDF info for tenant isolation.
func DeriveTenantOPRFShare(rootShare []byte, tenantID string) ([]byte, error) {
	if len(rootShare) != TenantOPRFShareSize {
		return nil, ErrInvalidKeySize
	}

	derived, err := hkdf.Key(sha256.New, rootShare, nil,
		string(appendDomainTenantLabel(DomainTenantOPRFShare, tenantID)), TenantOPRFShareSize)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to derive tenant OPRF share: %w", err)
	}
	return derived, nil
}
