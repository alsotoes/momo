package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	// ChunkSize is the default plaintext chunk length for streaming AEAD. It is
	// the historical value so existing callers observe identical behavior unless
	// a different size is set via SetStreamChunkSize.
	ChunkSize = 4096

	// MinChunkSize is the lower bound for a configurable stream chunk size
	// (Rule 32 — bounded allocation).
	MinChunkSize = 512

	// ChunkHeader is the width of a chunk's big-endian length prefix.
	ChunkHeader = 4

	// MaxChunkSize bounds a stream chunk's full encoded length (plaintext chunk
	// + length prefix + nonce + AEAD tag). It is the decoder's allocation cap.
	MaxChunkSize = ChunkSize + ChunkHeader + NonceSize + TagSize

	// headerVersionBytes is the width of the stream version field; the v4
	// header is [version][chunkSizeHi][chunkSizeLo].
	headerVersionBytes = 1
	// chunkSizeFieldBytes is the width of the big-endian chunk-size field in
	// the v4 stream header.
	chunkSizeFieldBytes = 2
	// streamHeaderSize is the total v4 header length (version + chunk size).
	streamHeaderSize = headerVersionBytes + chunkSizeFieldBytes

	// StreamVersion is the current wire stream format. v4 adds a self-describing
	// chunk-size field so the decoder can validate backups bounds before
	// allocating and accept streams written with a different chunk size.
	StreamVersion = 4

	// streamSeedSize is the number of random bytes seeded per stream. It is
	// XORed into the first part of the nonce so that a (key, nonce) pair is
	// never reused across streams for the same key.
	streamSeedSize = 8

	// StreamVersionLegacy2 is the prior stream format that had no integrity
	// footer. It is still decoded (insecure against truncation) for backward
	// compatibility with blobs written before the footer was added.
	StreamVersionLegacy2 = 2

	// StreamVersionLegacy3 is the immediate predecessor format (fixed 4096
	// chunks with an integrity footer, 1-byte header). It is still decoded for
	// backward compatibility with blobs written before v4.
	StreamVersionLegacy3 = 3
)

// streamFooterAAD is the AEAD additional authenticated data bound to the
// stream footer chunk. It is intentionally domain-separated from data chunks
// (whose AAD is nil) so DecryptStream can distinguish the final footer from a
// regular ciphertext chunk.
var streamFooterAAD = []byte("momo:stream-footer:v1")

var (
	ErrStreamFormat    = &streamError{posix: unix.EBADMSG, domain: errors.New("crypto: invalid stream format")}
	ErrStreamTruncated = &streamError{posix: unix.EIO, domain: errors.New("crypto: stream truncated (integrity footer missing)")}
)

// streamError is a domain error that also unwraps to a POSIX constant so the
// storage layer can match on both the stream sentinel and the underlying
// syscall via errors.Is (Rule 10/42).
type streamError struct {
	posix  error
	domain error
}

func (e *streamError) Error() string {
	return e.domain.Error()
}

func (e *streamError) Unwrap() []error {
	return []error{e.posix, e.domain}
}

func (e *streamError) Is(target error) bool {
	return target == e.posix || target == e.domain
}

// wrapStreamErr annotates a stream error with context while preserving
// errors.Is matching against both the POSIX constant and the domain sentinel.
func wrapStreamErr(e error, format string, args ...any) error {
	if se, ok := e.(*streamError); ok {
		return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), se)
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), e)
}

// recoverStreamErr is the unified panic-recovery helper for the streaming
// cipher functions. It maps any panic to syscall.EIO so an unexpected runtime
// panic cannot crash the daemon (Rule 37).
func recoverStreamErr(err *error, op string) {
	if r := recover(); r != nil {
		log.Printf("CRITICAL: Panic recovered in %s: %v", op, r)
		*err = fmt.Errorf("panic in %s: %v: %w", op, r, syscall.EIO)
	}
}

// footerNonceIndex marks the AEAD nonce index used for the stream footer. It is
// deliberately distinct from any data-chunk index so an attacker cannot forge
// or reorder a footer as a regular chunk, and vice versa.
const footerNonceIndex = 0xFFFFFFFF

func (c *Cipher) EncryptStream(plaintext io.Reader, dst io.Writer) (err error) {
	defer recoverStreamErr(&err, "EncryptStream")

	chunk := c.chunkSize
	if chunk < MinChunkSize || chunk > MaxChunkSize {
		// Defensive: the field is set via NewCipher/SetStreamChunkSize which
		// both validate, but never allow an unbounded allocation (Rule 4/32).
		chunk = ChunkSize
	}

	// v4 header: [version][chunkSizeHi][chunkSizeLo].
	var header [streamHeaderSize]byte
	header[0] = StreamVersion
	header[1] = byte(chunk >> 8)
	header[2] = byte(chunk)
	if _, err := dst.Write(header[:]); err != nil {
		return fmt.Errorf("crypto: failed to write stream header: %w", err)
	}

	seed := make([]byte, streamSeedSize)
	if _, err := io.ReadFull(rand.Reader, seed); err != nil {
		return fmt.Errorf("crypto: failed to generate stream seed: %w", err)
	}

	if _, err := dst.Write(seed); err != nil {
		return fmt.Errorf("crypto: failed to write stream seed: %w", err)
	}

	buf := make([]byte, chunk)
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
	return writeStreamFooter(c, dst, seed, nonce, lenBuf, sealedBuf, buf, chunkIndex)
}

// writeStreamFooter appends the authenticated footer chunk for a stream that
// produced chunkCount data chunks. The plaintext container param is the
// loop's 4KB chunk buffer, reused so the footer adds no heap allocation.
func writeStreamFooter(c *Cipher, dst io.Writer, seed, nonce, lenBuf, sealedBuf, plainBuf []byte, chunkCount uint32) error {
	copy(nonce[0:streamSeedSize], seed)
	binary.BigEndian.PutUint32(nonce[streamSeedSize:], footerNonceIndex)

	binary.BigEndian.PutUint32(plainBuf[0:4], chunkCount)

	sealed := c.aead.Seal(sealedBuf[:0], nonce, plainBuf[0:4], streamFooterAAD)

	binary.BigEndian.PutUint32(lenBuf, uint32(len(sealed)))

	if _, err := dst.Write(lenBuf); err != nil {
		return fmt.Errorf("crypto: failed to write stream footer length: %w", err)
	}
	if _, err := dst.Write(sealed); err != nil {
		return fmt.Errorf("crypto: failed to write stream footer ciphertext: %w", err)
	}
	return nil
}

func (c *Cipher) DecryptStream(ciphertext io.Reader, dst io.Writer) (err error) {
	defer recoverStreamErr(&err, "DecryptStream")

	versionBuf := make([]byte, 1)
	if _, err := io.ReadFull(ciphertext, versionBuf); err != nil {
		return fmt.Errorf("crypto: failed to read stream header: %w", err)
	}

	switch versionBuf[0] {
	case StreamVersion:
		return c.decryptStreamV4(ciphertext, dst)
	case StreamVersionLegacy3:
		return c.decryptStreamV3(ciphertext, dst)
	case StreamVersionLegacy2:
		return c.decryptStreamLegacy(ciphertext, dst)
	default:
		return wrapStreamErr(ErrStreamFormat, "unsupported version %d", versionBuf[0])
	}
}

// decryptStreamV4 decodes the current (v4) format, which carries a
// self-describing chunk-size field so the decoder can validate the bound and
// allocate accordingly. Framing after the header matches v3 (per-chunk length
// prefix, AEAD seal, integrity footer).
func (c *Cipher) decryptStreamV4(ciphertext io.Reader, dst io.Writer) (err error) {
	defer recoverStreamErr(&err, "decryptStreamV4")

	var sizeField [chunkSizeFieldBytes]byte
	if _, err := io.ReadFull(ciphertext, sizeField[:]); err != nil {
		return fmt.Errorf("crypto: failed to read stream chunk-size header: %w", err)
	}
	declared := int(sizeField[0])<<8 | int(sizeField[1])
	if declared < MinChunkSize || declared > MaxChunkSize {
		return wrapStreamErr(ErrStreamFormat, "invalid v4 stream chunk size %d (must be within [%d, %d])", declared, MinChunkSize, MaxChunkSize)
	}

	seed := make([]byte, streamSeedSize)
	if _, err := io.ReadFull(ciphertext, seed); err != nil {
		return fmt.Errorf("crypto: failed to read stream seed: %w", err)
	}

	lenBuf := make([]byte, ChunkHeader)
	nonce := make([]byte, NonceSize)
	sealedBuf := make([]byte, MaxChunkSize)
	plaintextBuf := make([]byte, 0, declared)
	chunkIndex := uint32(0)

	for {
		sealedLen, err := readChunkLen(ciphertext, lenBuf)
		if err == io.EOF {
			// Encountered EOF before a footer: the stream was truncated.
			return wrapStreamErr(ErrStreamTruncated, "expected integrity footer after %d chunk(s)", chunkIndex)
		}
		if err != nil {
			return err
		}

		if sealedLen > uint32(MaxChunkSize) {
			return wrapStreamErr(ErrStreamFormat, "invalid chunk size %d", sealedLen)
		}
		if sealedLen == 0 {
			return wrapStreamErr(ErrStreamFormat, "invalid zero-length chunk")
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
			// May be the stream footer — retry with footer nonce/AAD.
			plaintext, err = c.aead.Open(plaintextBuf[:0], footerNonce(nonce, seed), sealed, streamFooterAAD)
			if err != nil {
				return ErrTampered
			}
			if len(plaintext) != 4 {
				return wrapStreamErr(ErrStreamFormat, "malformed stream footer")
			}
			footerCount := binary.BigEndian.Uint32(plaintext)
			if footerCount != chunkIndex {
				return wrapStreamErr(ErrStreamFormat, "footer chunk count %d != %d chunks decoded", footerCount, chunkIndex)
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

func (c *Cipher) decryptStreamLegacy(ciphertext io.Reader, dst io.Writer) (err error) {
	defer recoverStreamErr(&err, "decryptStreamLegacy")

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
			return wrapStreamErr(ErrStreamFormat, "invalid chunk size %d", sealedLen)
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

func (c *Cipher) decryptStreamV3(ciphertext io.Reader, dst io.Writer) (err error) {
	defer recoverStreamErr(&err, "decryptStreamV3")

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
			return wrapStreamErr(ErrStreamTruncated, "expected integrity footer after %d chunk(s)", chunkIndex)
		}
		if err != nil {
			return err
		}

		if sealedLen > uint32(MaxChunkSize) {
			return wrapStreamErr(ErrStreamFormat, "invalid chunk size %d", sealedLen)
		}
		if sealedLen == 0 {
			return wrapStreamErr(ErrStreamFormat, "invalid zero-length chunk")
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
				return wrapStreamErr(ErrStreamFormat, "malformed stream footer")
			}
			footerCount := binary.BigEndian.Uint32(plaintext)
			if footerCount != chunkIndex {
				return wrapStreamErr(ErrStreamFormat, "footer chunk count %d != %d chunks decoded", footerCount, chunkIndex)
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
