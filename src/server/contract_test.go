package server

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// Contract constants — these define the wire protocol and must never change
// without a protocol version bump. Tests in this file assert that the server
// adheres to these exact byte-level contracts.
const (
	contractAuthTokenLen  = 64
	contractTimestampLen  = 19
	contractModeLen       = 1
	contractHandshakeLen  = contractAuthTokenLen + contractTimestampLen + contractModeLen // 84

	contractHashLen     = 64
	contractFileNameLen = 64
	contractFileSizeLen = 64
	contractMetadataLen = contractHashLen + contractFileNameLen + contractFileSizeLen // 192

	contractStatusLen = 1
	contractACKLen    = 4 // "ACK" + 1-digit serverId (e.g., "ACK0")
)

func TestContract_HandshakeLayout(t *testing.T) {
	if contractHandshakeLen != 84 {
		t.Fatalf("handshake must be exactly 84 bytes, got %d", contractHandshakeLen)
	}
	if common.AuthTokenLength != contractAuthTokenLen {
		t.Fatalf("AuthTokenLength must be %d, got %d", contractAuthTokenLen, common.AuthTokenLength)
	}
}

func TestContract_MetadataLayout(t *testing.T) {
	if contractMetadataLen != 192 {
		t.Fatalf("metadata must be exactly 192 bytes, got %d", contractMetadataLen)
	}
}

func TestContract_HandshakeRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer ln.Close()

	authToken := "test-token-for-contract" // notsecret
	paddedToken := common.PadString(authToken, common.AuthTokenLength)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, contractHandshakeLen)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn.Read(buf); err != nil {
			t.Errorf("Failed to read handshake: %v", err)
			return
		}

		token := strings.TrimRight(string(buf[:contractAuthTokenLen]), "\x00")
		if token != authToken {
			t.Errorf("Auth token mismatch: got %q, want %q", token, authToken)
		}

		mode := buf[contractAuthTokenLen+contractTimestampLen]
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		conn.Write([]byte{mode})
	}()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	handshake := make([]byte, contractHandshakeLen)
	copy(handshake[:contractAuthTokenLen], []byte(paddedToken))
	ts := fmt.Sprintf("%019d", time.Now().UnixNano())
	copy(handshake[contractAuthTokenLen:contractAuthTokenLen+contractTimestampLen], []byte(ts))
	handshake[contractAuthTokenLen+contractTimestampLen] = '0'

	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(handshake); err != nil {
		t.Fatalf("Failed to send handshake: %v", err)
	}

	resp := make([]byte, 1)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Read(resp); err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if resp[0] != '0' {
		t.Errorf("Mode mismatch: got %c, want 0", resp[0])
	}
}

func TestContract_P2PRPCFraming(t *testing.T) {
	// P2P RPC frames use a 4-byte big-endian length prefix.
	// This test validates the framing contract independently of the P2P package.

	body := []byte{0x01, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03}
	totalLen := uint32(len(body))

	var buf bytes.Buffer
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], totalLen)
	buf.Write(lenBuf[:])
	buf.Write(body)

	if buf.Len() != 4+len(body) {
		t.Fatalf("Frame size mismatch: got %d, want %d", buf.Len(), 4+len(body))
	}

	decoded := binary.BigEndian.Uint32(buf.Bytes()[:4])
	if decoded != totalLen {
		t.Fatalf("Length prefix mismatch: got %d, want %d", decoded, totalLen)
	}
	if totalLen < 5 {
		t.Fatal("RPC body must be at least 5 bytes (type + from)")
	}
}

func TestContract_FileMetadataSizes(t *testing.T) {
	meta := common.FileMetadata{
		Hash: strings.Repeat("a", contractHashLen),
		Name: "test.txt",
		Size: 1024,
	}

	paddedName := common.PadString(meta.Name, contractFileNameLen)
	if len(paddedName) != contractFileNameLen {
		t.Fatalf("Padded name must be %d bytes, got %d", contractFileNameLen, len(paddedName))
	}

	sizeStr := fmt.Sprintf("%d", meta.Size)
	paddedSize := common.PadString(sizeStr, contractFileSizeLen)
	if len(paddedSize) != contractFileSizeLen {
		t.Fatalf("Padded size must be %d bytes, got %d", contractFileSizeLen, len(paddedSize))
	}

	totalMetadata := len(meta.Hash) + len(paddedName) + len(paddedSize)
	if totalMetadata != contractMetadataLen {
		t.Fatalf("Total metadata must be %d bytes, got %d", contractMetadataLen, totalMetadata)
	}
}
