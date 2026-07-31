package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/transport"
)

func padTestString(input string, length int) string {
	if len(input) >= length {
		return input[:length]
	}
	return input + string(make([]byte, length-len(input)))
}

func TestChangeReplicationModeServerReal(t *testing.T) {
	daemons := []*common.Daemon{
		{ChangeReplication: "127.0.0.1:45678"},
		{ChangeReplication: "127.0.0.1:45679"},
		{ChangeReplication: "127.0.0.1:45680"},
	}
	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	cfg := common.Configuration{
		Daemons: daemons,
		Global: common.ConfigurationGlobal{
			AuthToken: authToken,
			Protocol:  "momo-tcp",
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l1, _ := net.Listen("tcp", "127.0.0.1:45679")
	defer l1.Close()
	go func() {
		conn, err := l1.Accept()
		if err == nil {
			defer conn.Close()
			// ⚡ Bolt: Read the full 84-byte handshake
			authBuf := make([]byte, common.AuthTokenLength+common.TimestampLength+1)
			io.ReadFull(conn, authBuf)
			// Send back a replication mode to complete handshake
			conn.Write([]byte("0"))

			// ⚡ Bolt: Consume the JSON payload and send OK
			buf := make([]byte, 1024)
			conn.Read(buf)
			conn.Write([]byte("OK"))
		}
	}()

	l2, _ := net.Listen("tcp", "127.0.0.1:45680")
	defer l2.Close()
	go func() {
		conn, err := l2.Accept()
		if err == nil {
			defer conn.Close()
			// ⚡ Bolt: Read the full 84-byte handshake
			authBuf := make([]byte, common.AuthTokenLength+common.TimestampLength+1)
			io.ReadFull(conn, authBuf)
			// Send back a replication mode to complete handshake
			conn.Write([]byte("0"))

			// ⚡ Bolt: Consume the JSON payload and send OK
			buf := make([]byte, 1024)
			conn.Read(buf)
			conn.Write([]byte("OK"))
		}
	}()

	go ChangeReplicationModeServer(ctx, cfg, 0, time.Now().UnixNano())

	// 🛡️ Zero-Crash: Use a retry loop to wait for the replication server to bind.
	var conn net.Conn
	var err error
	for i := 0; i < 10; i++ {
		conn, err = net.Dial("tcp", "127.0.0.1:45678")
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err != nil {
		t.Fatalf("Failed to dial test server after retries: %v", err)
	}
	defer conn.Close()

	comm := transport.NewMomoTCPCommunicator(conn)
	if _, err := comm.HandshakeClient(authToken, time.Now().UnixNano(), 0); err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	data := common.ReplicationData{
		New:       common.ReplicationSplay,
		TimeStamp: time.Now().Unix(),
	}
	jsonBytes, _ := json.Marshal(data)
	comm.Write(jsonBytes)
	time.Sleep(100 * time.Millisecond)
}

func TestDaemonReal(t *testing.T) {
	tempDir := t.TempDir()
	daemons := []*common.Daemon{
		{Host: "127.0.0.1:45681", Data: tempDir + "/001"},
		{Host: "127.0.0.1:45682", Data: tempDir + "/002"},
		{Host: "127.0.0.1:45683", Data: tempDir + "/003"},
	}

	for _, d := range daemons {
		os.MkdirAll(d.Data, 0755)
	}

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	cfg := common.Configuration{
		Daemons: daemons,
		Global: common.ConfigurationGlobal{
			AuthToken: authToken,
			Protocol:  "momo-tcp",
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go Daemon(ctx, cfg, 0)

	// 🛡️ Zero-Crash: Use a robust retry loop instead of a fixed sleep to wait for the daemon to bind.
	var conn net.Conn
	var err error
	for i := 0; i < 10; i++ {
		conn, err = net.Dial("tcp", "127.0.0.1:45681")
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err != nil {
		t.Fatalf("Failed to dial Daemon test server after retries: %v", err)
	}
	defer conn.Close()

	comm := transport.NewMomoTCPCommunicator(conn)
	if _, err := comm.HandshakeClient(authToken, 1234567890123456789, 0); err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	file, err := os.CreateTemp("", "test_daemon_file_*.txt")
	if err == nil {
		file.WriteString("data")
		file.Close()
		hash, _ := common.HashFile(file.Name())
		meta := &common.FileMetadata{
			Name: "test.txt",
			Hash: hash,
			Size: 4,
		}
		status, err := comm.SendMetadata(meta)
		if err != nil {
			t.Fatalf("Failed to send metadata: %v", err)
		}
		if status != transport.MetadataStatusSendPayload {
			t.Fatalf("Expected status %d, got %d", transport.MetadataStatusSendPayload, status)
		}

		comm.Write([]byte("data"))

		if err := comm.ReceiveACK(); err != nil {
			t.Logf("Failed to read ACK from server: %v", err)
		}
	}
}
