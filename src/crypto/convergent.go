package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"fmt"
	"io"
)

type ConvergentResult struct {
	Ciphertext []byte
	ContentHash []byte
}

func ConvergentEncrypt(plaintext []byte, pepper []byte) (*ConvergentResult, error) {
	h := sha256.New()
	h.Write(plaintext)
	if len(pepper) > 0 {
		h.Write(pepper)
	}
	contentKey := h.Sum(nil)

	block, err := aes.NewCipher(contentKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create convergent cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create GCM: %w", err)
	}

	nonce := make([]byte, NonceSize)
	ciphertext := aead.Seal(nonce, nonce, plaintext, nil)

	return &ConvergentResult{
		Ciphertext:  ciphertext,
		ContentHash: contentKey,
	}, nil
}

func ConvergentDecrypt(ciphertext []byte, contentHash []byte) ([]byte, error) {
	if len(contentHash) != KeySize {
		return nil, ErrInvalidKeySize
	}

	cipher, err := NewCipher(contentHash)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create convergent cipher: %w", err)
	}

	plaintext, err := cipher.Decrypt(ciphertext)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

func ConvergentEncryptStream(plaintext io.Reader, pepper []byte) (*ConvergentResult, error) {
	data, err := io.ReadAll(plaintext)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to read plaintext for convergent encryption: %w", err)
	}

	return ConvergentEncrypt(data, pepper)
}
