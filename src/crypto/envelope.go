package crypto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"syscall"
)

// Envelope magic and versioning. The envelope is a self-describing, versioned
// wrapper that is embedded as a prefix of an S3 object's bytes. Because the
// envelope travels with the object, the S3 gateway (and any momo peer) can
// treat the whole object as opaque ciphertext: it never sees the plaintext and
// needs no per-object metadata to route the envelope.
const (
	// EnvelopeMagic is the 8-byte prefix identifying a momo E2EE envelope.
	EnvelopeMagic = "MOMOENV1"

	// EnvelopeVersion is the envelope schema version.
	EnvelopeVersion byte = 0x01

	// EnvelopeAlgoAES256GCM marks an AES-256-GCM wrapped data key (the only
	// algorithm currently supported).
	EnvelopeAlgoAES256GCM byte = 0x01

	// envelopeMaxKeyIDLen bounds the key-id length parsed from the header to
	// prevent memory exhaustion on a malformed/crafted object (Rule 24).
	envelopeMaxKeyIDLen = 256

	// envelopeMaxWrappedKeyLen bounds the wrapped-data-key length. A wrapped
	// AES-256 data key is nonce(12) + key(32) + tag(16) = 60 bytes; a generous
	// bound still guards a crafted header allocating a huge slice.
	envelopeMaxWrappedKeyLen = 1024

	envelopeMagicLen = len(EnvelopeMagic)
	// envelopeFixedLen is the header size after the magic: version(1) +
	// algorithm(1) + keyIDLen(2) + wrappedKeyLen(2).
	envelopeFixedLen = 6
)

// ErrNotEnvelope indicates the reader does not begin with a momo envelope magic.
var ErrNotEnvelope = errors.New("crypto: not a momo E2EE envelope")

// EnvelopeMeta describes the header of a parsed envelope.
type EnvelopeMeta struct {
	Version   byte
	Algorithm byte
	KeyID     string
}

// wrapEnvelopeRecover recovers panics in envelope operations to syscall.EIO.
func wrapEnvelopeRecover(err *error, op string) {
	if r := recover(); r != nil {
		log.Printf("CRITICAL: Panic recovered in %s: %v", op, r)
		*err = fmt.Errorf("panic in %s: %v: %w", op, r, syscall.EIO)
	}
}

// EncryptEnvelope generates a fresh AES-256 data key, wraps it with masterKey
// (AES-GCM-256 via Cipher.Encrypt), writes a self-describing header to dst,
// then streams plaintext through the standard momo streaming AEAD under the
// data key. The client holds masterKey; it must never be shared with the
// server (zero-trust vs the serving node, issue #777).
func EncryptEnvelope(dst io.Writer, plaintext io.Reader, masterKey []byte, keyID string) (err error) {
	defer wrapEnvelopeRecover(&err, "EncryptEnvelope")

	if len(masterKey) != KeySize {
		return ErrInvalidKeySize
	}
	if len(keyID) > envelopeMaxKeyIDLen {
		return fmt.Errorf("crypto: key-id %d bytes exceeds maximum %d: %w", len(keyID), envelopeMaxKeyIDLen, syscall.EINVAL)
	}

	dataKey, err := GenerateKey()
	if err != nil {
		return fmt.Errorf("crypto: failed to generate data key: %w", err)
	}

	wrapCipher, err := NewCipher(masterKey)
	if err != nil {
		return fmt.Errorf("crypto: failed to create wrapping cipher: %w", err)
	}
	wrappedKey, err := wrapCipher.Encrypt(dataKey)
	if err != nil {
		return fmt.Errorf("crypto: failed to wrap data key: %w", err)
	}

	if err := writeEnvelopeHeader(dst, EnvelopeVersion, EnvelopeAlgoAES256GCM, keyID, wrappedKey); err != nil {
		return err
	}

	dataCipher, err := NewCipher(dataKey)
	if err != nil {
		return fmt.Errorf("crypto: failed to create data cipher: %w", err)
	}
	return dataCipher.EncryptStream(plaintext, dst)
}

// writeEnvelopeHeader writes the self-describing envelope prefix.
func writeEnvelopeHeader(dst io.Writer, version, algorithm byte, keyID string, wrappedKey []byte) error {
	if _, err := io.WriteString(dst, EnvelopeMagic); err != nil {
		return fmt.Errorf("crypto: failed to write envelope magic: %w", err)
	}
	if len(wrappedKey) > envelopeMaxWrappedKeyLen {
		return fmt.Errorf("crypto: wrapped key %d bytes exceeds maximum %d: %w", len(wrappedKey), envelopeMaxWrappedKeyLen, syscall.EINVAL)
	}

	var lenBuf [4]byte
	binary.BigEndian.PutUint16(lenBuf[0:2], uint16(len(keyID)))
	binary.BigEndian.PutUint16(lenBuf[2:4], uint16(len(wrappedKey)))

	hdr := make([]byte, 0, 2+4+len(keyID)+len(wrappedKey))
	hdr = append(hdr, version, algorithm)
	hdr = append(hdr, lenBuf[:]...)
	hdr = append(hdr, keyID...)
	hdr = append(hdr, wrappedKey...)

	if _, err := dst.Write(hdr); err != nil {
		return fmt.Errorf("crypto: failed to write envelope header: %w", err)
	}
	return nil
}

// DecryptEnvelope parses the self-describing envelope prefix from src, unwraps
// the data key with masterKey, and streams the ciphertext through the standard
// momo streaming AEAD into dst. Any tampering of the header or ciphertext fails
// closed.
func DecryptEnvelope(src io.Reader, dst io.Writer, masterKey []byte) (EnvelopeMeta, error) {
	var meta EnvelopeMeta
	var err error

	defer wrapEnvelopeRecover(&err, "DecryptEnvelope")

	if len(masterKey) != KeySize {
		return meta, ErrInvalidKeySize
	}

	// ⚡ Bolt: Read the 8-byte magic and the fixed header fields with a single
	// bounded read to avoid calling ReadFull multiple times on the network.
	prefix := make([]byte, envelopeMagicLen+envelopeFixedLen)
	if _, err := io.ReadFull(src, prefix); err != nil {
		return meta, fmt.Errorf("crypto: failed to read envelope prefix: %w", err)
	}

	if string(prefix[:envelopeMagicLen]) != EnvelopeMagic {
		return meta, ErrNotEnvelope
	}

	meta.Version = prefix[envelopeMagicLen]
	meta.Algorithm = prefix[envelopeMagicLen+1]
	if meta.Version != EnvelopeVersion {
		return meta, fmt.Errorf("crypto: unsupported envelope version 0x%02x: %w", meta.Version, syscall.EBADMSG)
	}
	if meta.Algorithm != EnvelopeAlgoAES256GCM {
		return meta, fmt.Errorf("crypto: unsupported envelope algorithm 0x%02x: %w", meta.Algorithm, syscall.EBADMSG)
	}

	keyIDLen := int(binary.BigEndian.Uint16(prefix[envelopeMagicLen+2 : envelopeMagicLen+4]))
	wrappedLen := int(binary.BigEndian.Uint16(prefix[envelopeMagicLen+4 : envelopeMagicLen+6]))

	if keyIDLen > envelopeMaxKeyIDLen || wrappedLen > envelopeMaxWrappedKeyLen {
		return meta, fmt.Errorf("crypto: envelope header field too large (key-id %d, wrapped %d): %w",
			keyIDLen, wrappedLen, syscall.EBADMSG)
	}

	body := make([]byte, keyIDLen+wrappedLen)
	if _, err := io.ReadFull(src, body); err != nil {
		return meta, fmt.Errorf("crypto: failed to read envelope key material: %w", err)
	}

	meta.KeyID = string(body[:keyIDLen])
	wrappedKey := body[keyIDLen:]

	wrapCipher, err := NewCipher(masterKey)
	if err != nil {
		return meta, fmt.Errorf("crypto: failed to create unwrapping cipher: %w", err)
	}
	dataKey, err := wrapCipher.Decrypt(wrappedKey)
	if err != nil {
		return meta, fmt.Errorf("crypto: failed to unwrap data key (wrong master key or tampered header): %w", err)
	}

	dataCipher, err := NewCipher(dataKey)
	if err != nil {
		return meta, fmt.Errorf("crypto: failed to create data cipher: %w", err)
	}
	if err := dataCipher.DecryptStream(src, dst); err != nil {
		return meta, fmt.Errorf("crypto: failed to decrypt envelope content: %w", err)
	}

	return meta, nil
}

// IsEnvelope reports whether the leading bytes begin with the momo E2EE
// envelope magic. It reads at most len(EnvelopeMagic) bytes from r.
func IsEnvelope(r io.Reader) (bool, error) {
	prefix := make([]byte, envelopeMagicLen)
	n, err := io.ReadFull(r, prefix)
	if err != nil && err != io.ErrUnexpectedEOF {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	return string(prefix[:n]) == EnvelopeMagic, nil
}
