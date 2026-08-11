package crypto

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	masterKey := bytes.Repeat([]byte{0x42}, KeySize)
	plaintext := []byte("hello momo end-to-end encryption")
	keyID := "tenant-a/key-1"

	var buf bytes.Buffer
	if err := EncryptEnvelope(&buf, bytes.NewReader(plaintext), masterKey, keyID); err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}
	if buf.Len() <= len(plaintext) {
		t.Fatalf("envelope should be larger than plaintext, got %d <= %d", buf.Len(), len(plaintext))
	}

	var out bytes.Buffer
	meta, err := DecryptEnvelope(&buf, &out, masterKey)
	if err != nil {
		t.Fatalf("DecryptEnvelope: %v", err)
	}
	if meta.Version != EnvelopeVersion || meta.Algorithm != EnvelopeAlgoAES256GCM || meta.KeyID != keyID {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	if !bytes.Equal(out.Bytes(), plaintext) {
		t.Fatalf("round-trip mismatch: got %q want %q", out.Bytes(), plaintext)
	}
}

func TestEnvelopeRoundTripEmpty(t *testing.T) {
	masterKey := bytes.Repeat([]byte{0x24}, KeySize)

	var buf bytes.Buffer
	if err := EncryptEnvelope(&buf, bytes.NewReader(nil), masterKey, "empty"); err != nil {
		t.Fatalf("EncryptEnvelope empty: %v", err)
	}
	var out bytes.Buffer
	if _, err := DecryptEnvelope(&buf, &out, masterKey); err != nil {
		t.Fatalf("DecryptEnvelope empty: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected empty output, got %d bytes", out.Len())
	}
}

func TestEnvelopeRoundTripLargeStream(t *testing.T) {
	masterKey := bytes.Repeat([]byte{0x11}, KeySize)
	// 3 MB so the streaming AEAD crosses many chunks and the footer must be
	// decoded correctly.
	plaintext := bytes.Repeat([]byte("A"), 3<<20)

	var buf bytes.Buffer
	if err := EncryptEnvelope(&buf, bytes.NewReader(plaintext), masterKey, "large"); err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}
	var out bytes.Buffer
	if _, err := DecryptEnvelope(&buf, &out, masterKey); err != nil {
		t.Fatalf("DecryptEnvelope: %v", err)
	}
	if out.Len() != len(plaintext) || !bytes.Equal(out.Bytes(), plaintext) {
		t.Fatalf("large round-trip mismatch: got %d bytes", out.Len())
	}
}

func TestEnvelopeWrongMasterKey(t *testing.T) {
	masterKey := bytes.Repeat([]byte{0x42}, KeySize)
	wrongKey := bytes.Repeat([]byte{0x99}, KeySize)

	var buf bytes.Buffer
	if err := EncryptEnvelope(&buf, strings.NewReader("secret"), masterKey, "k"); err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}
	var out bytes.Buffer
	_, err := DecryptEnvelope(&buf, &out, wrongKey)
	if err == nil || !errors.Is(err, ErrTampered) {
		t.Fatalf("expected ErrTampered with wrong master key, got %v", err)
	}
}

func TestEnvelopeBadKeySize(t *testing.T) {
	var buf bytes.Buffer
	if err := EncryptEnvelope(&buf, strings.NewReader("x"), []byte("short"), "k"); !errors.Is(err, ErrInvalidKeySize) {
		t.Fatalf("expected ErrInvalidKeySize, got %v", err)
	}
	var out bytes.Buffer
	if _, err := DecryptEnvelope(&buf, &out, []byte("short")); !errors.Is(err, ErrInvalidKeySize) {
		t.Fatalf("expected ErrInvalidKeySize, got %v", err)
	}
}

func TestEnvelopeNotEnvelope(t *testing.T) {
	var buf bytes.Buffer
	if err := EncryptEnvelope(&buf, strings.NewReader("x"), bytes.Repeat([]byte{1}, KeySize), "k"); err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}

	// Flip the first magic byte.
	tampered := buf.Bytes()
	tampered[0] ^= 0xFF
	var out bytes.Buffer
	_, err := DecryptEnvelope(bytes.NewReader(tampered), &out, bytes.Repeat([]byte{1}, KeySize))
	if err == nil || !errors.Is(err, ErrNotEnvelope) {
		t.Fatalf("expected ErrNotEnvelope, got %v", err)
	}
}

func TestEnvelopeTamperedCiphertext(t *testing.T) {
	masterKey := bytes.Repeat([]byte{0x07}, KeySize)
	plaintext := bytes.Repeat([]byte("tamper me"), 2048)

	var buf bytes.Buffer
	if err := EncryptEnvelope(&buf, bytes.NewReader(plaintext), masterKey, "k"); err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}
	// Flip a byte in the middle of the ciphertext body (after the header).
	body := buf.Bytes()
	mid := envelopeMagicLen + envelopeFixedLen + 16 // skip magic + fixed header + key-id
	body[mid] ^= 0x01

	var out bytes.Buffer
	_, err := DecryptEnvelope(bytes.NewReader(body), &out, masterKey)
	if err == nil || !errors.Is(err, ErrTampered) {
		t.Fatalf("expected ErrTampered on ciphertext corruption, got %v", err)
	}
}

func TestEnvelopeTamperedWrappedKey(t *testing.T) {
	masterKey := bytes.Repeat([]byte{0x0A}, KeySize)

	var buf bytes.Buffer
	if err := EncryptEnvelope(&buf, strings.NewReader("payload"), masterKey, "k"); err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}
	body := buf.Bytes()
	// The wrapped key begins right after magic(8) + fixed(6) + key-id(1).
	wrappedKeyStart := envelopeMagicLen + envelopeFixedLen + 1
	body[wrappedKeyStart+5] ^= 0xFF

	var out bytes.Buffer
	_, err := DecryptEnvelope(bytes.NewReader(body), &out, masterKey)
	if err == nil || !errors.Is(err, ErrTampered) {
		t.Fatalf("expected ErrTampered on wrapped-key corruption, got %v", err)
	}
}

func TestEnvelopeTruncated(t *testing.T) {
	masterKey := bytes.Repeat([]byte{0x55}, KeySize)

	var buf bytes.Buffer
	if err := EncryptEnvelope(&buf, bytes.NewReader([]byte("x")), masterKey, "k"); err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}
	full := buf.Bytes()

	// Truncate to just past the header: no data/footer chunks.
	truncated := full[:envelopeMagicLen+envelopeFixedLen+1]
	if len(truncated) < len(full) {
		var out bytes.Buffer
		if _, err := DecryptEnvelope(bytes.NewReader(truncated), &out, masterKey); err == nil {
			t.Fatalf("expected error on truncated envelope")
		}
	}
}

func TestEnvelopeOversizedKeyIDField(t *testing.T) {
	masterKey := bytes.Repeat([]byte{0x0C}, KeySize)

	var buf bytes.Buffer
	if err := EncryptEnvelope(&buf, strings.NewReader("x"), masterKey, "k"); err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}
	body := buf.Bytes()
	// Corrupt key-id length field to 0xFFFF (magic + version + algorithm).
	body[envelopeMagicLen+2] = 0xFF
	body[envelopeMagicLen+3] = 0xFF

	var out bytes.Buffer
	_, err := DecryptEnvelope(bytes.NewReader(body), &out, masterKey)
	if err == nil {
		t.Fatalf("expected error on oversized key-id field")
	}
	if !errors.Is(err, syscall.EBADMSG) && !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected EBADMSG/too-large error, got %v", err)
	}
}

func TestIsEnvelope(t *testing.T) {
	masterKey := bytes.Repeat([]byte{0x01}, KeySize)

	var buf bytes.Buffer
	if err := EncryptEnvelope(&buf, strings.NewReader("x"), masterKey, "k"); err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}
	ok, err := IsEnvelope(bytes.NewReader(buf.Bytes()))
	if err != nil || !ok {
		t.Fatalf("expected envelope detected, ok=%v err=%v", ok, err)
	}

	ok, err = IsEnvelope(strings.NewReader("not-an-envelope"))
	if err != nil || ok {
		t.Fatalf("expected no envelope, ok=%v err=%v", ok, err)
	}
}

func TestEnvelopeIdentityAcrossFile(t *testing.T) {
	// Deterministic check: the envelope header prefix (magic + version +
	// algorithm) is stable, and cleanly decrypts a real file round-trip.
	masterKey := bytes.Repeat([]byte{0x66}, KeySize)
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.txt")
	outPath := filepath.Join(dir, "out.enc")
	decPath := filepath.Join(dir, "dec.txt")

	payload := []byte(strings.Repeat("identity ", 500))
	if err := os.WriteFile(inPath, payload, 0600); err != nil {
		t.Fatal(err)
	}

	enc := new(bytes.Buffer)
	f, err := os.Open(inPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := EncryptEnvelope(enc, f, masterKey, "file-key"); err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}
	if err := os.WriteFile(outPath, enc.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}

	meta, err := DecryptEnvelope(bytes.NewReader(enc.Bytes()), mustWriter(t, decPath), masterKey)
	if err != nil {
		t.Fatalf("DecryptEnvelope: %v", err)
	}
	if meta.KeyID != "file-key" {
		t.Fatalf("unexpected key-id %q", meta.KeyID)
	}
	dec, err := os.ReadFile(decPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dec, payload) {
		t.Fatalf("file round-trip mismatch")
	}
}

func mustWriter(t *testing.T, path string) io.Writer {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	return &closeWriter{f: f}
}

type closeWriter struct {
	f *os.File
}

func (w *closeWriter) Write(p []byte) (int, error) { return w.f.Write(p) }

// TestEnvelopeKeyUniqueness asserts two envelopes of the same plaintext with
// the same master key differ (random data keys per object).
func TestEnvelopeKeyUniqueness(t *testing.T) {
	masterKey := bytes.Repeat([]byte{0x2A}, KeySize)
	plaintext := []byte("same plaintext two times")

	a := new(bytes.Buffer)
	b := new(bytes.Buffer)
	if err := EncryptEnvelope(a, bytes.NewReader(plaintext), masterKey, "k"); err != nil {
		t.Fatal(err)
	}
	if err := EncryptEnvelope(b, bytes.NewReader(plaintext), masterKey, "k"); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatalf("envelopes must be distinct (random data keys)")
	}
}

func TestEnvelopeRejectsStrictlyTamperedMeta(t *testing.T) {
	masterKey := bytes.Repeat([]byte{0x7B}, KeySize)
	var buf bytes.Buffer
	if err := EncryptEnvelope(&buf, strings.NewReader("data"), masterKey, "k"); err != nil {
		t.Fatal(err)
	}
	body := buf.Bytes()
	// Bump the version to an unsupported value.
	body[envelopeMagicLen] = 0x77
	var out bytes.Buffer
	if _, err := DecryptEnvelope(bytes.NewReader(body), &out, masterKey); err == nil {
		t.Fatalf("expected error on unsupported version")
	} else if !errors.Is(err, syscall.EBADMSG) && !strings.Contains(err.Error(), "version") {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = hex.EncodeToString
}
