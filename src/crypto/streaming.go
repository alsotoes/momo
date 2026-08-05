package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	ChunkSize     = 4096
	ChunkHeader   = 4
	MaxChunkSize  = ChunkSize + ChunkHeader + NonceSize + TagSize
	StreamVersion = 2

	// streamSeedSize is the number of random bytes seeded per stream. It is
	// XORed into the first part of the nonce so that a (key, nonce) pair is
	// never reused across streams for the same key.
	streamSeedSize = 8
)

var (
	ErrStreamFormat = errors.New("crypto: invalid stream format")
)

func (c *Cipher) EncryptStream(plaintext io.Reader, dst io.Writer) error {
	header := []byte{StreamVersion}
	if _, err := dst.Write(header); err != nil {
		return fmt.Errorf("crypto: failed to write stream header: %w", err)
	}

	seed := make([]byte, streamSeedSize)
	if _, err := io.ReadFull(rand.Reader, seed); err != nil {
		return fmt.Errorf("crypto: failed to generate stream seed: %w", err)
	}

	if _, err := dst.Write(seed); err != nil {
		return fmt.Errorf("crypto: failed to write stream seed: %w", err)
	}

	buf := make([]byte, ChunkSize)
	nonce := make([]byte, NonceSize)
	lenBuf := make([]byte, ChunkHeader)
	sealedBuf := make([]byte, 0, MaxChunkSize)
	chunkIndex := uint32(0)

	for {
		n, err := io.ReadFull(plaintext, buf)
		if err == io.EOF {
			break
		}
		if err != nil && err != io.ErrUnexpectedEOF {
			return fmt.Errorf("crypto: failed to read plaintext chunk: %w", err)
		}

		isLast := err == io.ErrUnexpectedEOF

		copy(nonce[0:streamSeedSize], seed)
		binary.BigEndian.PutUint32(nonce[streamSeedSize:], chunkIndex)

		sealed := c.aead.Seal(sealedBuf[:0], nonce, buf[:n], nil)

		binary.BigEndian.PutUint32(lenBuf, uint32(len(sealed)))

		if _, err := dst.Write(lenBuf); err != nil {
			return fmt.Errorf("crypto: failed to write chunk length: %w", err)
		}
		if _, err := dst.Write(sealed); err != nil {
			return fmt.Errorf("crypto: failed to write chunk ciphertext: %w", err)
		}

		chunkIndex++

		if isLast {
			break
		}
	}

	if chunkIndex == 0 {
		copy(nonce[0:streamSeedSize], seed)
		sealed := c.aead.Seal(sealedBuf[:0], nonce, nil, nil)

		binary.BigEndian.PutUint32(lenBuf, uint32(len(sealed)))

		if _, err := dst.Write(lenBuf); err != nil {
			return fmt.Errorf("crypto: failed to write empty chunk length: %w", err)
		}
		if _, err := dst.Write(sealed); err != nil {
			return fmt.Errorf("crypto: failed to write empty chunk ciphertext: %w", err)
		}
	}

	return nil
}

func (c *Cipher) DecryptStream(ciphertext io.Reader, dst io.Writer) error {
	versionBuf := make([]byte, 1)
	if _, err := io.ReadFull(ciphertext, versionBuf); err != nil {
		return fmt.Errorf("crypto: failed to read stream header: %w", err)
	}

	if versionBuf[0] != StreamVersion {
		return fmt.Errorf("%w: unsupported version %d", ErrStreamFormat, versionBuf[0])
	}

	seed := make([]byte, streamSeedSize)
	if _, err := io.ReadFull(ciphertext, seed); err != nil {
		return fmt.Errorf("crypto: failed to read stream seed: %w", err)
	}

	lenBuf := make([]byte, ChunkHeader)
	nonce := make([]byte, NonceSize)
	sealedBuf := make([]byte, MaxChunkSize)
	plaintextBuf := make([]byte, 0, ChunkSize)
	chunkIndex := uint32(0)

	for {
		_, err := io.ReadFull(ciphertext, lenBuf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("crypto: failed to read chunk length: %w", err)
		}

		sealedLen := binary.BigEndian.Uint32(lenBuf)
		if sealedLen == 0 || sealedLen > uint32(MaxChunkSize) {
			return fmt.Errorf("%w: invalid chunk size %d", ErrStreamFormat, sealedLen)
		}

		sealed := sealedBuf[:sealedLen]
		if _, err := io.ReadFull(ciphertext, sealed); err != nil {
			return fmt.Errorf("crypto: failed to read chunk ciphertext: %w", err)
		}

		copy(nonce[0:streamSeedSize], seed)
		binary.BigEndian.PutUint32(nonce[streamSeedSize:], chunkIndex)

		plaintext, err := c.aead.Open(plaintextBuf[:0], nonce, sealed, nil)
		if err != nil {
			return ErrTampered
		}

		if _, err := dst.Write(plaintext); err != nil {
			return fmt.Errorf("crypto: failed to write decrypted chunk: %w", err)
		}

		chunkIndex++
	}

	return nil
}
