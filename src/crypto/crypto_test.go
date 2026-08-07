package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"golang.org/x/sys/unix"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	c, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher failed: %v", err)
	}

	tests := [][]byte{
		[]byte(""),
		[]byte("hello world"),
		bytes.Repeat([]byte("a"), 4096),
		bytes.Repeat([]byte("b"), 4097),
		bytes.Repeat([]byte("c"), 100*1024),
	}

	for i, plaintext := range tests {
		ciphertext, err := c.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("test %d: Encrypt failed: %v", i, err)
		}

		decrypted, err := c.Decrypt(ciphertext)
		if err != nil {
			t.Fatalf("test %d: Decrypt failed: %v", i, err)
		}

		if !bytes.Equal(plaintext, decrypted) {
			t.Fatalf("test %d: round-trip mismatch", i)
		}
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	plaintext := []byte("same plaintext")

	ct1, _ := c.Encrypt(plaintext)
	ct2, _ := c.Encrypt(plaintext)

	if bytes.Equal(ct1, ct2) {
		t.Fatal("same plaintext produced identical ciphertexts (nonce not random)")
	}
}

func TestDecryptTampered(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	plaintext := []byte("sensitive data")
	ciphertext, _ := c.Encrypt(plaintext)

	ciphertext[len(ciphertext)-1] ^= 0xFF

	_, err := c.Decrypt(ciphertext)
	if err != ErrTampered {
		t.Fatalf("expected ErrTampered, got: %v", err)
	}
}

func TestDecryptTooShort(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	_, err := c.Decrypt([]byte("short"))
	if err != ErrCiphertextTooShort {
		t.Fatalf("expected ErrCiphertextTooShort, got: %v", err)
	}
}

func TestNewCipherInvalidKeySize(t *testing.T) {
	_, err := NewCipher([]byte("too short"))
	if err != ErrInvalidKeySize {
		t.Fatalf("expected ErrInvalidKeySize, got: %v", err)
	}
}

func TestNewCipherFromHex(t *testing.T) {
	key, _ := GenerateKey()
	hexKey := hex.EncodeToString(key)

	c, err := NewCipherFromHex(hexKey)
	if err != nil {
		t.Fatalf("NewCipherFromHex failed: %v", err)
	}

	plaintext := []byte("test")
	ciphertext, _ := c.Encrypt(plaintext)
	decrypted, _ := c.Decrypt(ciphertext)

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatal("hex key round-trip failed")
	}
}

func TestNewCipherFromHexInvalid(t *testing.T) {
	_, err := NewCipherFromHex("short")
	if err != ErrInvalidHexKey {
		t.Fatalf("expected ErrInvalidHexKey, got: %v", err)
	}

	_, err = NewCipherFromHex("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestDeriveKey(t *testing.T) {
	master, _ := GenerateKey()

	k1, err := DeriveKey(master, "tenant-a", nil)
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}

	k2, err := DeriveKey(master, "tenant-b", nil)
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}

	if bytes.Equal(k1, k2) {
		t.Fatal("different tenants produced same derived key")
	}

	k3, err := DeriveKey(master, "tenant-a", nil)
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}

	if !bytes.Equal(k1, k3) {
		t.Fatal("same tenant produced different derived keys")
	}
}

func TestDeriveKeyInvalidMaster(t *testing.T) {
	_, err := DeriveKey([]byte("short"), "tenant", nil)
	if err != ErrInvalidKeySize {
		t.Fatalf("expected ErrInvalidKeySize, got: %v", err)
	}
}

func TestDeriveKeyWithContext(t *testing.T) {
	master, _ := GenerateKey()

	k1, _ := DeriveKey(master, "tenant", []byte("context-a"))
	k2, _ := DeriveKey(master, "tenant", []byte("context-b"))

	if bytes.Equal(k1, k2) {
		t.Fatal("different contexts produced same derived key")
	}
}

func TestDeriveKeyConcatenationAmbiguity(t *testing.T) {
	master, _ := GenerateKey()

	// Length-prefixed info must distinguish ("ab","c") from ("a","bc").
	k1, _ := DeriveKey(master, "ab", []byte("c"))
	k2, _ := DeriveKey(master, "a", []byte("bc"))

	if bytes.Equal(k1, k2) {
		t.Fatal("DeriveKey is ambiguous across concatenation boundaries")
	}
}

func TestDeriveKeyDomainSeparation(t *testing.T) {
	master, _ := GenerateKey()

	// Each domain label must produce a distinct key for the same tenant.
	domains := [][]byte{DomainToken, DomainContent, DomainAtRest, DomainOPRF}
	seen := make(map[string]bool)
	for _, d := range domains {
		k, _ := DeriveKey(master, "tenant-a", d)
		h := hex.EncodeToString(k)
		if seen[h] {
			t.Fatalf("domain %q produced a duplicate key", string(d))
		}
		seen[h] = true
	}
}

func TestEncryptStreamNonceNotReused(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	// A plaintext spanning multiple chunks must use distinct nonces.
	plaintext := bytes.Repeat([]byte("n"), 4*ChunkSize)
	var encBuf bytes.Buffer
	if err := c.EncryptStream(bytes.NewReader(plaintext), &encBuf); err != nil {
		t.Fatalf("EncryptStream failed: %v", err)
	}

	// Re-encrypting the same plaintext must use distinct seeds (and nonces).
	var encBuf2 bytes.Buffer
	if err := c.EncryptStream(bytes.NewReader(plaintext), &encBuf2); err != nil {
		t.Fatalf("EncryptStream failed: %v", err)
	}

	// The stream seeds differ, so no nonce can collide.
	if bytes.Equal(encBuf.Bytes()[1:1+streamSeedSize], encBuf2.Bytes()[1:1+streamSeedSize]) {
		t.Fatal("two encryptions of the same stream reused the same seed")
	}
}

func TestDecryptStreamRejectsUnsupportedVersion(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	// A v1 stream predates versioned formats; it must be rejected.
	legacy := append([]byte{1}, bytes.Repeat([]byte{0xAA}, ChunkSize+NonceSize)...)
	var decBuf bytes.Buffer
	if err := c.DecryptStream(bytes.NewReader(legacy), &decBuf); !errors.Is(err, ErrStreamFormat) {
		t.Fatalf("expected ErrStreamFormat for unsupported stream version, got: %v", err)
	}
}

// TestDecryptStreamLegacyV2StillDecodes verifies that v2 blob fragments (which
// predate the integrity footer) remain decryptable so pre-existing at-rest
// blobs do not become unreadable after an upgrade.
func TestDecryptStreamLegacyV2StillDecodes(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	// Craft a minimal valid v2 stream by hand: version byte 2, a fixed seed,
	// then a single AEAD-sealed data chunk with nil AAD (no footer).
	seed := bytes.Repeat([]byte{0x11}, streamSeedSize)
	nonce := make([]byte, NonceSize)
	copy(nonce[0:streamSeedSize], seed)
	binary.BigEndian.PutUint32(nonce[streamSeedSize:], 0)

	plaintext := []byte("legacy-blob-data")
	sealed := c.aead.Seal(nil, nonce, plaintext, nil)

	var stream bytes.Buffer
	stream.WriteByte(StreamVersionLegacy2)
	stream.Write(seed)
	var lenBuf [ChunkHeader]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(sealed)))
	stream.Write(lenBuf[:])
	stream.Write(sealed)

	var decBuf bytes.Buffer
	if err := c.DecryptStream(&stream, &decBuf); err != nil {
		t.Fatalf("DecryptStream rejected legacy v2 stream: %v", err)
	}
	if !bytes.Equal(decBuf.Bytes(), plaintext) {
		t.Fatalf("v2 decode mismatch: got %q, want %q", decBuf.Bytes(), plaintext)
	}
}

// TestDecryptStreamDetectsTruncation ensures a v3 stream whose trailing chunks
// (including the integrity footer) are dropped is rejected instead of silently
// returning partial plaintext.
func TestDecryptStreamDetectsTruncation(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	plaintext := bytes.Repeat([]byte("T"), 3*ChunkSize+100)
	var encBuf, decBuf bytes.Buffer
	if err := c.EncryptStream(bytes.NewReader(plaintext), &encBuf); err != nil {
		t.Fatalf("EncryptStream failed: %v", err)
	}

	// Drop the trailing footer chunk (4-byte length + sealed count + tag) to
	// simulate truncation. The stream must then end cleanly at a data-chunk
	// boundary so DecryptStream hits EOF while expecting the footer.
	footerBytes := ChunkHeader + 4 + TagSize
	truncated := encBuf.Bytes()[:len(encBuf.Bytes())-footerBytes]

	if err := c.DecryptStream(bytes.NewReader(truncated), &decBuf); !errors.Is(err, ErrStreamTruncated) {
		t.Fatalf("expected ErrStreamTruncated, got: %v", err)
	}
}

// TestDecryptStreamRejectsFooterChunkCountMismatch ensures a forged footer that
// authenticates with the wrong chunk count is rejected.
func TestDecryptStreamRejectsFooterChunkCountMismatch(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	plaintext := bytes.Repeat([]byte("C"), 2*ChunkSize)
	var encBuf, decBuf bytes.Buffer
	if err := c.EncryptStream(bytes.NewReader(plaintext), &encBuf); err != nil {
		t.Fatalf("EncryptStream failed: %v", err)
	}

	buf := encBuf.Bytes()

	// Locate and re-seal the footer with an incorrect chunk count.
	seed := buf[1 : 1+streamSeedSize]
	nonce := make([]byte, NonceSize)
	copy(nonce[0:streamSeedSize], seed)
	binary.BigEndian.PutUint32(nonce[streamSeedSize:], footerNonceIndex)

	var wrong [4]byte
	binary.BigEndian.PutUint32(wrong[:], 12345)
	badFooter := c.aead.Seal(nil, nonce, wrong[:], streamFooterAAD)

	var forged bytes.Buffer
	forged.Write(buf[:len(buf)-(ChunkHeader+4+TagSize)]) // drop original footer
	var flen [ChunkHeader]byte
	binary.BigEndian.PutUint32(flen[:], uint32(len(badFooter)))
	forged.Write(flen[:])
	forged.Write(badFooter)

	if err := c.DecryptStream(&forged, &decBuf); !errors.Is(err, ErrStreamFormat) {
		t.Fatalf("expected ErrStreamFormat for footer count mismatch, got: %v", err)
	}
}

// TestDecryptStreamErrorsMapToPOSIXConsts verifies that protocol errors surface
// the POSIX constants required by Rule 10/42 via errors.Is, so the storage
// layer can react to them.
func TestDecryptStreamErrorsMapToPOSIXConsts(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	var decBuf bytes.Buffer
	// Unsupported version -> ErrStreamFormat -> syscall.EBADMSG on Linux.
	err := c.DecryptStream(bytes.NewReader([]byte{1, 0xAA}), &decBuf)
	if !errors.Is(err, ErrStreamFormat) {
		t.Fatalf("expected ErrStreamFormat, got: %v", err)
	}
	if !errors.Is(err, unix.EBADMSG) {
		t.Fatalf("expected errors.Is(err, unix.EBADMSG), got: %v", err)
	}

	// Truncated stream -> ErrStreamTruncated -> syscall.EIO.
	plaintext := bytes.Repeat([]byte("R"), 2*ChunkSize)
	var encBuf bytes.Buffer
	c.EncryptStream(bytes.NewReader(plaintext), &encBuf)
	footerBytes := ChunkHeader + 4 + TagSize
	truncated := encBuf.Bytes()[:len(encBuf.Bytes())-footerBytes]
	decBuf.Reset()
	truncErr := c.DecryptStream(bytes.NewReader(truncated), &decBuf)
	if !errors.Is(truncErr, ErrStreamTruncated) {
		t.Fatalf("expected ErrStreamTruncated, got: %v", truncErr)
	}
	if !errors.Is(truncErr, unix.EIO) {
		t.Fatalf("expected errors.Is(err, unix.EIO), got: %v", truncErr)
	}
}

func TestEncryptStreamDecryptStreamRoundTrip(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	tests := [][]byte{
		[]byte(""),
		[]byte("hello"),
		bytes.Repeat([]byte("x"), ChunkSize),
		bytes.Repeat([]byte("y"), ChunkSize+1),
		bytes.Repeat([]byte("z"), 10*ChunkSize),
		bytes.Repeat([]byte("w"), 10*ChunkSize+500),
	}

	for i, plaintext := range tests {
		var encBuf, decBuf bytes.Buffer

		if err := c.EncryptStream(bytes.NewReader(plaintext), &encBuf); err != nil {
			t.Fatalf("test %d: EncryptStream failed: %v", i, err)
		}

		if err := c.DecryptStream(&encBuf, &decBuf); err != nil {
			t.Fatalf("test %d: DecryptStream failed: %v", i, err)
		}

		if !bytes.Equal(plaintext, decBuf.Bytes()) {
			t.Fatalf("test %d: stream round-trip mismatch (len %d vs %d)", i, len(plaintext), decBuf.Len())
		}
	}
}

func TestDecryptStreamTampered(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	plaintext := bytes.Repeat([]byte("A"), 2*ChunkSize)
	var encBuf bytes.Buffer
	c.EncryptStream(bytes.NewReader(plaintext), &encBuf)

	// Header (1) + seed (8) + first chunk length (4) = 13 bytes; flip a byte
	// inside the first chunk's GCM payload so the auth tag fails.
	encBuf.Bytes()[13+ChunkSize/2] ^= 0xFF

	var decBuf bytes.Buffer
	err := c.DecryptStream(&encBuf, &decBuf)
	if err != ErrTampered {
		t.Fatalf("expected ErrTampered, got: %v", err)
	}
}

func TestGenerateKeyRandomness(t *testing.T) {
	k1, _ := GenerateKey()
	k2, _ := GenerateKey()

	if bytes.Equal(k1, k2) {
		t.Fatal("GenerateKey produced identical keys")
	}

	if len(k1) != KeySize {
		t.Fatalf("key size = %d, want %d", len(k1), KeySize)
	}
}

func TestLargePayloadRoundTrip(t *testing.T) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	plaintext := make([]byte, 1024*1024)
	rand.Read(plaintext)

	ciphertext, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := c.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatal("1MB round-trip mismatch")
	}
}
