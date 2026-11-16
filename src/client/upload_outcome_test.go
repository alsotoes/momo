package client

import (
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/transport"
)

// uploadTestToken is the shared cluster token for these tests. // notsecret
const uploadTestToken = "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"

// startMockDaemon runs a minimal momo-tcp peer: it completes the handshake,
// reports the given replication mode, accepts one object's metadata, and either
// acknowledges the payload (ack=true) or closes without an ACK. Closing without
// an ACK is what a daemon does when it could not store the object.
func startMockDaemon(t *testing.T, mode int, ack bool) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, accErr := ln.Accept()
			if accErr != nil {
				return
			}
			go serveMockDaemon(conn, mode, ack)
		}
	}()
	return ln.Addr().String()
}

// serveMockDaemon answers one connection: handshake, metadata, payload, ACK.
func serveMockDaemon(c net.Conn, mode int, ack bool) {
	defer func() { _ = c.Close() }()
	if !mockHandshake(c, mode) {
		return
	}
	size, ok := mockMetadata(c)
	if !ok {
		return
	}
	if size > 0 {
		if _, err := io.ReadFull(c, make([]byte, size)); err != nil {
			return
		}
	}
	if ack {
		_, _ = c.Write([]byte("ACK0"))
	}
}

// mockHandshake consumes the handshake frame and reports the replication mode.
func mockHandshake(c net.Conn, mode int) bool {
	frame := make([]byte, common.AuthTokenLength+common.TimestampLength+1)
	if _, err := io.ReadFull(c, frame); err != nil {
		return false
	}
	_, err := c.Write([]byte(strconv.Itoa(mode)))
	return err == nil
}

// mockMetadata reads one metadata frame, tells the client to send the payload,
// and returns the announced payload size.
func mockMetadata(c net.Conn) (int, bool) {
	meta := make([]byte, 64+common.FileInfoLength+common.FileInfoLength)
	if _, err := io.ReadFull(c, meta); err != nil {
		return 0, false
	}
	if _, err := c.Write([]byte{transport.MetadataStatusSendPayload}); err != nil {
		return 0, false
	}
	sizeStr := strings.TrimRight(string(meta[64+common.FileInfoLength:]), "\x00")
	size, err := strconv.Atoi(sizeStr)
	if err != nil || size < 0 || size > common.MaxFileSize {
		return 0, false
	}
	return size, true
}

// uploadTempFile creates a small file for an upload attempt.
func uploadTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "upload-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, wErr := f.WriteString(content); wErr != nil {
		t.Fatalf("write temp file: %v", wErr)
	}
	if cErr := f.Close(); cErr != nil {
		t.Fatalf("close temp file: %v", cErr)
	}
	return f.Name()
}

// uploadOnce runs one client.Connect and returns its result.
func uploadOnce(t *testing.T, cfg common.Configuration, path string, serverID, factor int) error {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(1)
	err := Connect(&wg, cfg, path, "", serverID, time.Now().UnixNano(), 0, factor)
	wg.Wait()
	return err
}

// TestUploadOutcome pins the caller-visible result of a client fan-out: zero
// acknowledged replicas stored nothing and must be an error (Rule 9); partial
// success is degraded, not failed (#1344).
func TestUploadOutcome(t *testing.T) {
	cases := []struct {
		name    string
		total   int
		acked   int
		wantIs  error
		wantErr bool
	}{
		{"no replica connected", 0, 0, syscall.ENOTCONN, true},
		{"all replicas failed", 3, 0, syscall.EIO, true},
		{"partial success is degraded", 3, 1, nil, false},
		{"complete success", 3, 3, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := uploadOutcome(tc.total, tc.acked)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("expected success, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error")
			}
			if !errors.Is(err, tc.wantIs) {
				t.Fatalf("expected %v, got %v", tc.wantIs, err)
			}
		})
	}
}

// TestConnect_PrimaryUnreachableFails proves an unreachable primary is a
// failure, not a logged no-op that still exits successfully.
func TestConnect_PrimaryUnreachableFails(t *testing.T) {
	cfg := common.Configuration{
		Global:  common.ConfigurationGlobal{AuthToken: uploadTestToken, Protocol: "momo-tcp"},
		Daemons: []*common.Daemon{{Host: "127.0.0.1:59997"}},
	}
	err := uploadOnce(t, cfg, uploadTempFile(t, "unreachable"), 0, 1)
	if !errors.Is(err, syscall.ENOTCONN) {
		t.Fatalf("expected ENOTCONN for an unreachable primary, got %v", err)
	}
}

// TestConnect_AllReplicasFailFails proves a replica that does not acknowledge
// the payload (it stored nothing) turns into an upload failure.
func TestConnect_AllReplicasFailFails(t *testing.T) {
	addr := startMockDaemon(t, common.ReplicationNone, false)
	cfg := common.Configuration{
		Global:  common.ConfigurationGlobal{AuthToken: uploadTestToken, Protocol: "momo-tcp"},
		Daemons: []*common.Daemon{{Host: addr}},
	}
	err := uploadOnce(t, cfg, uploadTempFile(t, "rejected"), 0, 1)
	if !errors.Is(err, syscall.EIO) {
		t.Fatalf("expected EIO when the only replica withheld its ACK, got %v", err)
	}
}

// TestConnect_PartialSuccessSucceeds proves one stored replica is still a
// success: the bytes are durable somewhere and the repair loop can restore the
// target count.
func TestConnect_PartialSuccessSucceeds(t *testing.T) {
	primary := startMockDaemon(t, common.ReplicationPrimarySplay, true)
	rejecting := startMockDaemon(t, common.ReplicationNone, false)
	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			AuthToken:         uploadTestToken,
			Protocol:          "momo-tcp",
			ReplicationFactor: 2,
		},
		Daemons: []*common.Daemon{{Host: primary}, {Host: rejecting}},
	}
	if err := uploadOnce(t, cfg, uploadTempFile(t, "partial"), 0, 2); err != nil {
		t.Fatalf("one acknowledged replica must not fail the upload: %v", err)
	}
}
