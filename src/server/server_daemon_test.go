package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"testing"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
)

func padTestString(input string, length int) string {
	if len(input) >= length {
		return input[:length]
	}
	return input + string(make([]byte, length-len(input)))
}

func TestChangeReplicationModeServerReal(t *testing.T) {
	daemons := []*momo_common.Daemon{
		{ChangeReplication: "127.0.0.1:45678"},
		{ChangeReplication: "127.0.0.1:45679"},
		{ChangeReplication: "127.0.0.1:45680"},
	}
	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
	cfg := momo_common.Configuration{
		Daemons: daemons,
		Global: momo_common.ConfigurationGlobal{
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
			authBuf := make([]byte, momo_common.AuthTokenLength)
			io.ReadFull(conn, authBuf)
		}
	}()

	l2, _ := net.Listen("tcp", "127.0.0.1:45680")
	defer l2.Close()
	go func() {
		conn, err := l2.Accept()
		if err == nil {
			defer conn.Close()
			authBuf := make([]byte, momo_common.AuthTokenLength)
			io.ReadFull(conn, authBuf)
		}
	}()

	go ChangeReplicationModeServer(ctx, cfg, 0, time.Now().UnixNano())
	time.Sleep(100 * time.Millisecond)

	conn, err := net.Dial("tcp", "127.0.0.1:45678")
	if err != nil {
		t.Fatalf("Failed to dial test server: %v", err)
	}
	defer conn.Close()

	comm := momo_common.NewMomoTCPCommunicator(conn)
	if _, err := comm.HandshakeClient(authToken, time.Now().UnixNano()); err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	data := momo_common.ReplicationData{
		New:       momo_common.ReplicationSplay,
		TimeStamp: time.Now().Unix(),
	}
	jsonBytes, _ := json.Marshal(data)
	comm.Write(jsonBytes)
	time.Sleep(100 * time.Millisecond)
}

func TestDaemonReal(t *testing.T) {
	tempDir := t.TempDir()
	daemons := []*momo_common.Daemon{
		{Host: "127.0.0.1:45681", Data: tempDir + "/001"},
		{Host: "127.0.0.1:45682", Data: tempDir + "/002"},
		{Host: "127.0.0.1:45683", Data: tempDir + "/003"},
	}

	for _, d := range daemons {
		os.MkdirAll(d.Data, 0755)
	}

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
	cfg := momo_common.Configuration{
		Daemons: daemons,
		Global: momo_common.ConfigurationGlobal{
			AuthToken: authToken,
			Protocol:  "momo-tcp",
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go Daemon(ctx, cfg, 0)
	time.Sleep(100 * time.Millisecond)

	conn, err := net.Dial("tcp", "127.0.0.1:45681")
	if err != nil {
		t.Fatalf("Failed to dial Daemon test server: %v", err)
	}
	defer conn.Close()

	comm := momo_common.NewMomoTCPCommunicator(conn)
	if _, err := comm.HandshakeClient(authToken, 1234567890123456789); err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	file, err := os.CreateTemp("", "test_daemon_file_*.txt")
	if err == nil {
		file.WriteString("data")
		file.Close()
		hash, _ := momo_common.HashFile(file.Name())
		meta := &momo_common.FileMetadata{
			Name: "test.txt",
			Hash: hash,
			Size: 4,
		}
		if err := comm.SendMetadata(meta); err != nil {
			t.Fatalf("Failed to send metadata: %v", err)
		}

		comm.Write([]byte("data"))

		if err := comm.ReceiveACK(); err != nil {
			t.Logf("Failed to read ACK from server: %v", err)
		}
	}
}
