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
)

const (
	KeySize       = 32
	NonceSize     = 12
	TagSize       = 16
	MaxKeyHexSize = 64
)

var (
	ErrInvalidKeySize     = errors.New("crypto: key must be 32 bytes")
	ErrInvalidNonceSize   = errors.New("crypto: nonce must be 12 bytes")
	ErrCiphertextTooShort = errors.New("crypto: ciphertext too short")
	ErrTampered           = errors.New("crypto: authentication failed (tampered data)")
	ErrInvalidHexKey      = errors.New("crypto: encryption_key must be 64 hex characters (256-bit)")
)

type Cipher struct {
	aead cipher.AEAD
}

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

	return &Cipher{aead: aead}, nil
}

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

func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: failed to generate nonce: %w", err)
	}

	ciphertext := c.aead.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

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

func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("crypto: failed to generate key: %w", err)
	}
	return key, nil
}

func DeriveKey(masterKey []byte, tenant string, context []byte) ([]byte, error) {
	if len(masterKey) != KeySize {
		return nil, ErrInvalidKeySize
	}

	info := tenant
	if len(context) > 0 {
		info += string(context)
	}

	derived, err := hkdf.Key(sha256.New, masterKey, nil, info, KeySize)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to derive key: %w", err)
	}

	return derived, nil
}
