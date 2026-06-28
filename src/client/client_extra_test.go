package client

import (
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/transport"
)

func TestConnect_PrimarySplay(t *testing.T) {
	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // not a real token
	
	file, err := os.CreateTemp("", "test_splay_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(file.Name())
	file.WriteString("splay data")
	file.Close()

	// Setup 3 mock servers
	addr0, ln0 := startMockServerS3(t, authToken, common.ReplicationPrimarySplay)
	defer ln0.Close()
	addr1, ln1 := startMockServerS3(t, authToken, common.ReplicationNone) // mode doesn't matter for secondary
	defer ln1.Close()
	addr2, ln2 := startMockServerS3(t, authToken, common.ReplicationNone)
	defer ln2.Close()

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{AuthToken: authToken, Protocol: "momo-tcp"},
		Daemons: []*common.Daemon{
			{Host: addr0},
			{Host: addr1},
			{Host: addr2},
		},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	Connect(&wg, cfg, file.Name(), 0, time.Now().UnixNano(), 0, 3)
	wg.Wait()
}

func TestConnect_PrimarySplay_Failures(t *testing.T) {
	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // not a real token
	
	file, err := os.CreateTemp("", "test_splay_fail_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(file.Name())
	file.WriteString("splay data fail")
	file.Close()

	// Server 0 is primary, returns mode 3 (PrimarySplay)
	addr0, ln0 := startMockServerS3(t, authToken, common.ReplicationPrimarySplay)
	defer ln0.Close()

	// Server 1 is offline (we won't start a server on its port, Dial fails)
	addr1 := "127.0.0.1:59998"

	// Server 2 is active, but we will configure it with a wrong auth token (handshake fails)
	addr2, ln2 := startMockServerS3(t, "wrong_token_auth_token_wrong_token", common.ReplicationNone)
	defer ln2.Close()

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{AuthToken: authToken, Protocol: "momo-tcp"},
		Daemons: []*common.Daemon{
			{Host: addr0},
			{Host: addr1},
			{Host: addr2},
		},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	Connect(&wg, cfg, file.Name(), 0, time.Now().UnixNano(), 0, 3)
	wg.Wait()
}

func startMockServerS3(t *testing.T, authToken string, mode int) (string, net.Listener) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleMockConn(conn, authToken, mode)
		}
	}()
	return ln.Addr().String(), ln
}

func handleMockConn(conn net.Conn, authToken string, mode int) {
	defer conn.Close()
	
	// Handshake
	buf := make([]byte, common.AuthTokenLength+common.TimestampLength)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return
	}
	conn.Write([]byte(strconv.Itoa(mode)))

	// Metadata
	metaBuf := make([]byte, 64+common.FileInfoLength+common.FileInfoLength)
	if _, err := io.ReadFull(conn, metaBuf); err != nil {
		return
	}

	// Send metadata status
	conn.Write([]byte{transport.MetadataStatusSendPayload})

	// Payload (we don't know the exact size here, but we can read until EOF or just ACK)
	// For simplicity, just ACK after metadata
	conn.Write([]byte("ACK0"))
}
