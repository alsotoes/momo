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
	StreamVersion = 3

	// streamSeedSize is the number of random bytes seeded per stream. It is
	// XORed into the first part of the nonce so that a (key, nonce) pair is
	// never reused across streams for the same key.
	streamSeedSize = 8

	// StreamVersionLegacy2 is the prior stream format that had no integrity
	// footer. It is still decoded (insecure against truncation) for backward
	// compatibility with blobs written before the footer was added.
	StreamVersionLegacy2 = 2
)

// streamFooterAAD is the AEAD additional authenticated data bound to the
// stream footer chunk. It is intentionally domain-separated from data chunks
// (whose AAD is nil) so DecryptStream can distinguish the final footer from a
// regular ciphertext chunk.
var streamFooterAAD = []byte("momo:stream-footer:v1")

var (
	ErrStreamFormat    = errors.New("crypto: invalid stream format")
	ErrStreamTruncated = errors.New("crypto: stream truncated (integrity footer missing)")
)

// footerNonceIndex marks the AEAD nonce index used for the stream footer. It is
// deliberately distinct from any data-chunk index so an attacker cannot forge
// or reorder a footer as a regular chunk, and vice versa.
const footerNonceIndex = 0xFFFFFFFF

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

	// 🛡️ Stream integrity footer: a final AEAD-authenticated chunk that binds
	// the number of data chunks. DecryptStream requires it, so a stream that is
	// truncated (trailing chunks removed) now fails instead of silently
	// returning partial plaintext.
	return writeStreamFooter(c, dst, seed, nonce, lenBuf, sealedBuf, chunkIndex)
}

// writeStreamFooter appends the authenticated footer chunk for a stream that
// produced chunkCount data chunks.
func writeStreamFooter(c *Cipher, dst io.Writer, seed, nonce, lenBuf, sealedBuf []byte, chunkCount uint32) error {
	copy(nonce[0:streamSeedSize], seed)
	binary.BigEndian.PutUint32(nonce[streamSeedSize:], footerNonceIndex)

	var countBuf [4]byte
	binary.BigEndian.PutUint32(countBuf[:], chunkCount)

	sealed := c.aead.Seal(sealedBuf[:0], nonce, countBuf[:], streamFooterAAD)

	binary.BigEndian.PutUint32(lenBuf, uint32(len(sealed)))

	if _, err := dst.Write(lenBuf); err != nil {
		return fmt.Errorf("crypto: failed to write stream footer length: %w", err)
	}
	if _, err := dst.Write(sealed); err != nil {
		return fmt.Errorf("crypto: failed to write stream footer ciphertext: %w", err)
	}
	return nil
}

func (c *Cipher) DecryptStream(ciphertext io.Reader, dst io.Writer) error {
	versionBuf := make([]byte, 1)
	if _, err := io.ReadFull(ciphertext, versionBuf); err != nil {
		return fmt.Errorf("crypto: failed to read stream header: %w", err)
	}

	switch versionBuf[0] {
	case StreamVersion:
		return c.decryptStreamV3(ciphertext, dst)
	case StreamVersionLegacy2:
		return c.decryptStreamLegacy(ciphertext, dst)
	default:
		return fmt.Errorf("%w: unsupported version %d", ErrStreamFormat, versionBuf[0])
	}
}

func (c *Cipher) decryptStreamLegacy(ciphertext io.Reader, dst io.Writer) error {
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
			// Legacy format has no integrity footer — cannot detect truncation.
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

func (c *Cipher) decryptStreamV3(ciphertext io.Reader, dst io.Writer) error {
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
		sealedLen, err := readChunkLen(ciphertext, lenBuf)
		if err == io.EOF {
			// Encountered EOF before a footer: the stream was truncated.
			return fmt.Errorf("%w: expected integrity footer after %d chunk(s)", ErrStreamTruncated, chunkIndex)
		}
		if err != nil {
			return err
		}

		if sealedLen > uint32(MaxChunkSize) {
			return fmt.Errorf("%w: invalid chunk size %d", ErrStreamFormat, sealedLen)
		}
		if sealedLen == 0 {
			return fmt.Errorf("%w: invalid zero-length chunk", ErrStreamFormat)
		}

		sealed := sealedBuf[:sealedLen]
		if _, err := io.ReadFull(ciphertext, sealed); err != nil {
			return fmt.Errorf("crypto: failed to read chunk ciphertext: %w", err)
		}

		// Data chunk nonce.
		copy(nonce[0:streamSeedSize], seed)
		binary.BigEndian.PutUint32(nonce[streamSeedSize:], chunkIndex)

		plaintext, err := c.aead.Open(plaintextBuf[:0], nonce, sealed, nil)
		if err != nil {
			// Data-AAD verification failed. It might be the stream footer —
			// retry with the footer nonce/AAD before declaring tamper.
			plaintext, err = c.aead.Open(plaintextBuf[:0], footerNonce(nonce, seed), sealed, streamFooterAAD)
			if err != nil {
				return ErrTampered
			}
			// It is the footer. Verify it encodes the exact number of data
			// chunks seen, then end the stream.
			if len(plaintext) != 4 {
				return fmt.Errorf("%w: malformed stream footer", ErrStreamFormat)
			}
			footerCount := binary.BigEndian.Uint32(plaintext)
			if footerCount != chunkIndex {
				return fmt.Errorf("%w: footer chunk count %d != %d chunks decoded", ErrStreamFormat, footerCount, chunkIndex)
			}
			break
		}

		if _, err := dst.Write(plaintext); err != nil {
			return fmt.Errorf("crypto: failed to write decrypted chunk: %w", err)
		}

		chunkIndex++
	}

	return nil
}

// readChunkLen reads a sealed chunk's length. It returns io.EOF when the stream
// ends cleanly at an expectation of more data (used to detect truncation).
func readChunkLen(r io.Reader, lenBuf []byte) (uint32, error) {
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(lenBuf), nil
}

// footerNonce rebuilds the nonce for the stream footer inside the shared buffer.
func footerNonce(nonce, seed []byte) []byte {
	copy(nonce[0:streamSeedSize], seed)
	binary.BigEndian.PutUint32(nonce[streamSeedSize:], footerNonceIndex)
	return nonce
}
