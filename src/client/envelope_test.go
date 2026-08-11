package client

import (
	"bytes"
	"encoding/hex"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	momocrypto "github.com/alsotoes/momo/src/crypto"
	"github.com/alsotoes/momo/src/transport"
	"go.uber.org/goleak"
)

// TestConnect_EnvelopeStreamsToSpool is a regression test for issue #780: the
// native client must stream envelope-encrypted (zero-trust) content to a temp
// spool file rather than buffering ciphertext, the wire payload must be a
// self-describing momo E2EE envelope (starts with the envelope magic) and never
// contain the plaintext, and the temp spool must be cleaned up after upload.
func TestConnect_EnvelopeStreamsToSpool(t *testing.T) {
	defer goleak.VerifyNone(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	plaintext := "e2e-envelope-regression-test-780"
	masterKeyHex := strings.Repeat("b", 64)

	file, err := os.CreateTemp("", "test_env_stream_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(plaintext); err != nil {
		t.Fatalf("Failed to write plaintext: %v", err)
	}
	file.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	defer ln.Close()

	payloadCh := make(chan []byte, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Mock server panic recovered: %v", r)
			}
		}()

		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		bufAuth := make([]byte, common.AuthTokenLength)
		if _, err := io.ReadFull(conn, bufAuth); err != nil {
			return
		}

		buf := make([]byte, common.TimestampLength+1)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}

		conn.Write([]byte("0"))

		metaBuf := make([]byte, 64+common.FileInfoLength+common.FileInfoLength)
		if _, err := io.ReadFull(conn, metaBuf); err != nil {
			return
		}

		sizeStr := strings.TrimRight(string(metaBuf[64+common.FileInfoLength:]), "\x00")
		payloadSize, err := strconv.Atoi(sizeStr)
		if err != nil || payloadSize <= 0 || payloadSize > common.MaxFileSize {
			return
		}

		conn.Write([]byte{transport.MetadataStatusSendPayload})

		payload := make([]byte, payloadSize)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}

		conn.Write([]byte("ACK"))

		payloadCh <- payload
	}()

	cfg := common.Configuration{
		Daemons: []*common.Daemon{
			{Host: ln.Addr().String(), ChangeReplication: ln.Addr().String(), Data: "/tmp", Drive: "/dev/sda1"},
		},
		Global: common.ConfigurationGlobal{
			AuthToken:        authToken,
			Protocol:         "momo-tcp",
			E2EEKey:          masterKeyHex,
			E2EEKeyID:        "test-key",
			EncryptionTenant: "default",
		},
	}

	stale, _ := filepath.Glob(filepath.Join(os.TempDir(), "momo-enc-*"))
	for _, s := range stale {
		os.Remove(s)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	err = Connect(&wg, cfg, file.Name(), "", 0, time.Now().UnixNano(), 0, 3)
	wg.Wait()
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	select {
	case payload := <-payloadCh:
		if len(payload) == 0 {
			t.Fatal("Empty payload received")
		}
		if !bytes.HasPrefix(payload, []byte(momocrypto.EnvelopeMagic)) {
			t.Errorf("Expected payload to start with envelope magic %q, got %q", momocrypto.EnvelopeMagic, payload[:min(len(payload), len(momocrypto.EnvelopeMagic))])
		}
		if bytes.Contains(payload, []byte(plaintext)) {
			t.Error("FAIL: Plaintext leaked to wire (not envelope-encrypted)")
		}
		// The envelope must round-trip to plaintext under the client-held key,
		// proving the server never needs the key (zero-trust).
		masterKey, _ := hexDecodeMasterKey(t, masterKeyHex)
		var out bytes.Buffer
		if _, err := momocrypto.DecryptEnvelope(bytes.NewReader(payload), &out, masterKey); err != nil {
			t.Fatalf("Failed to decrypt captured envelope with client key: %v", err)
		}
		if out.String() != plaintext {
			t.Errorf("Decrypted envelope mismatch: got %q, want %q", out.String(), plaintext)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Mock server did not receive payload (timeout)")
	}

	leftover, _ := filepath.Glob(filepath.Join(os.TempDir(), "momo-enc-*"))
	if len(leftover) > 0 {
		t.Errorf("Temp spool files left behind: %v", leftover)
		for _, f := range leftover {
			os.Remove(f)
		}
	}
}

// TestDownload_EnvelopeRoundTrip is a regression test for issue #780: client
// Download must detect the self-describing envelope and decrypt it with the
// client-held E2EE key. It verifies the GET wire flow, envelope dispatch, and
// plaintext round-trip.
func TestDownload_EnvelopeRoundTrip(t *testing.T) {
	defer goleak.VerifyNone(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	plaintext := "e2e-download-envelope-roundtrip-780"
	masterKeyHex := strings.Repeat("c", 64)
	masterKey, err := hexDecodeMasterKey(t, masterKeyHex)

	// Build the envelope that the serving node stores (opaque to it).
	var envelope bytes.Buffer
	if err := momocrypto.EncryptEnvelope(&envelope, strings.NewReader(plaintext), masterKey, "test-key"); err != nil {
		t.Fatalf("Failed to encrypt envelope: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		bufAuth := make([]byte, common.AuthTokenLength)
		if _, err := io.ReadFull(conn, bufAuth); err != nil {
			return
		}
		buf := make([]byte, common.TimestampLength+1)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}

		reqBuf := make([]byte, 128)
		if _, err := io.ReadFull(conn, reqBuf); err != nil {
			return
		}

		// status '0' + 64-byte size
		var respBuf [65]byte
		respBuf[0] = '0'
		copy(respBuf[1:65], common.PadString(strconv.Itoa(envelope.Len()), 64))
		if _, err := conn.Write(respBuf[:]); err != nil {
			return
		}

		if _, err := conn.Write(envelope.Bytes()); err != nil {
			return
		}
	}()

	cfg := common.Configuration{
		Daemons: []*common.Daemon{
			{Host: ln.Addr().String(), ChangeReplication: ln.Addr().String(), Data: "/tmp", Drive: "/dev/sda1"},
		},
		Global: common.ConfigurationGlobal{
			AuthToken:        authToken,
			Protocol:         "momo-tcp",
			E2EEKey:          masterKeyHex,
			E2EEKeyID:        "test-key",
			EncryptionTenant: "default",
		},
	}

	var out bytes.Buffer
	if err := Download(cfg, "enc-name", "deadbeef", 0, &out); err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if out.String() != plaintext {
		t.Errorf("Decrypted content mismatch: got %q, want %q", out.String(), plaintext)
	}
}

// TestDownload_EnvelopeWrongKey verifies that decrypting an envelope with the
// wrong client-held key fails closed (zero-trust: only the correct key holder
// recovers plaintext).
func TestDownload_EnvelopeWrongKey(t *testing.T) {
	defer goleak.VerifyNone(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	plaintext := "e2e-download-envelope-wrongkey-780"
	encryptKey, _ := hexDecodeMasterKey(t, strings.Repeat("d", 64))
	wrongKey, _ := hexDecodeMasterKey(t, strings.Repeat("e", 64))

	var envelope bytes.Buffer
	if err := momocrypto.EncryptEnvelope(&envelope, strings.NewReader(plaintext), encryptKey, "test-key"); err != nil {
		t.Fatalf("Failed to encrypt envelope: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		bufAuth := make([]byte, common.AuthTokenLength)
		if _, err := io.ReadFull(conn, bufAuth); err != nil {
			return
		}
		buf := make([]byte, common.TimestampLength+1)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}

		reqBuf := make([]byte, 128)
		if _, err := io.ReadFull(conn, reqBuf); err != nil {
			return
		}

		var respBuf [65]byte
		respBuf[0] = '0'
		copy(respBuf[1:65], common.PadString(strconv.Itoa(envelope.Len()), 64))
		if _, err := conn.Write(respBuf[:]); err != nil {
			return
		}
		if _, err := conn.Write(envelope.Bytes()); err != nil {
			return
		}
	}()

	cfg := common.Configuration{
		Daemons: []*common.Daemon{
			{Host: ln.Addr().String(), ChangeReplication: ln.Addr().String(), Data: "/tmp", Drive: "/dev/sda1"},
		},
		Global: common.ConfigurationGlobal{
			AuthToken:        authToken,
			Protocol:         "momo-tcp",
			E2EEKey:          strings.Repeat("e", 64),
			E2EEKeyID:        "test-key",
			EncryptionTenant: "default",
		},
	}

	var out bytes.Buffer
	if err := Download(cfg, "enc-name", "deadbeef", 0, &out); err == nil {
		t.Fatal("Download succeeded with wrong E2EE key; expected failure")
	}
	_ = wrongKey
}

func hexDecodeMasterKey(t *testing.T, s string) ([]byte, error) {
	t.Helper()
	return hex.DecodeString(s)
}
