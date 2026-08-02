package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
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

	encBuf.Bytes()[10] ^= 0xFF

	var decBuf bytes.Buffer
	err := c.DecryptStream(&encBuf, &decBuf)
	if err != ErrTampered {
		t.Fatalf("expected ErrTampered, got: %v", err)
	}
}

func TestConvergentEncryptDecrypt(t *testing.T) {
	plaintext := []byte("dedup-friendly content")
	pepper := []byte("server-pepper")

	result, err := ConvergentEncrypt(plaintext, pepper)
	if err != nil {
		t.Fatalf("ConvergentEncrypt failed: %v", err)
	}

	decrypted, err := ConvergentDecrypt(result.Ciphertext, result.ContentHash)
	if err != nil {
		t.Fatalf("ConvergentDecrypt failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatal("convergent round-trip mismatch")
	}
}

func TestConvergentEncryptDedup(t *testing.T) {
	plaintext := []byte("same content for dedup")
	pepper := []byte("server-pepper")

	r1, _ := ConvergentEncrypt(plaintext, pepper)
	r2, _ := ConvergentEncrypt(plaintext, pepper)

	if !bytes.Equal(r1.Ciphertext, r2.Ciphertext) {
		t.Fatal("same plaintext produced different convergent ciphertexts (dedup broken)")
	}

	if !bytes.Equal(r1.ContentHash, r2.ContentHash) {
		t.Fatal("same plaintext produced different content hashes")
	}
}

func TestConvergentEncryptDifferentContent(t *testing.T) {
	pepper := []byte("server-pepper")

	r1, _ := ConvergentEncrypt([]byte("content-a"), pepper)
	r2, _ := ConvergentEncrypt([]byte("content-b"), pepper)

	if bytes.Equal(r1.ContentHash, r2.ContentHash) {
		t.Fatal("different plaintexts produced same content hash")
	}
}

func TestConvergentEncryptPepper(t *testing.T) {
	plaintext := []byte("same content")

	r1, _ := ConvergentEncrypt(plaintext, []byte("pepper-a"))
	r2, _ := ConvergentEncrypt(plaintext, []byte("pepper-b"))

	if bytes.Equal(r1.ContentHash, r2.ContentHash) {
		t.Fatal("different peppers produced same content hash (pepper not mixed in)")
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
