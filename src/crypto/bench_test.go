package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func BenchmarkEncrypt(b *testing.B) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	plaintext := make([]byte, 4096)
	rand.Read(plaintext)

	b.ResetTimer()
	b.SetBytes(int64(len(plaintext)))
	for i := 0; i < b.N; i++ {
		_, err := c.Encrypt(plaintext)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecrypt(b *testing.B) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	plaintext := make([]byte, 4096)
	rand.Read(plaintext)
	ciphertext, _ := c.Encrypt(plaintext)

	b.ResetTimer()
	b.SetBytes(int64(len(plaintext)))
	for i := 0; i < b.N; i++ {
		_, err := c.Decrypt(ciphertext)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncryptStream(b *testing.B) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	plaintext := make([]byte, 64*1024)
	rand.Read(plaintext)

	b.ResetTimer()
	b.SetBytes(int64(len(plaintext)))
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		if err := c.EncryptStream(bytes.NewReader(plaintext), &buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecryptStream(b *testing.B) {
	key, _ := GenerateKey()
	c, _ := NewCipher(key)

	plaintext := make([]byte, 64*1024)
	rand.Read(plaintext)

	var encBuf bytes.Buffer
	c.EncryptStream(bytes.NewReader(plaintext), &encBuf)
	ciphertext := encBuf.Bytes()

	b.ResetTimer()
	b.SetBytes(int64(len(plaintext)))
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		if err := c.DecryptStream(bytes.NewReader(ciphertext), &buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDeriveKey(b *testing.B) {
	master, _ := GenerateKey()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := DeriveKey(master, "tenant-a", nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}
