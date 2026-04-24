package server

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

func padTestString(input string, length int) string {
	if len(input) >= length {
		return input[:length]
	}
	return input + string(make([]byte, length-len(input)))
}

func TestChangeReplicationModeServerReal(t *testing.T) {
	defer goleak.VerifyNone(t)
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
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l1, _ := net.Listen("tcp", "127.0.0.1:45679")
	defer l1.Close()
	l2, _ := net.Listen("tcp", "127.0.0.1:45680")
	defer l2.Close()

	go ChangeReplicationModeServer(ctx, cfg, 0, time.Now().UnixNano())
	time.Sleep(100 * time.Millisecond)

	conn, err := net.Dial("tcp", "127.0.0.1:45678")
	if err != nil {
		t.Fatalf("Failed to dial test server: %v", err)
	}
	defer conn.Close()

	conn.Write([]byte(authToken))

	data := momo_common.ReplicationData{
		New:       momo_common.ReplicationSplay,
		TimeStamp: time.Now().Unix(),
	}
	jsonBytes, _ := json.Marshal(data)
	conn.Write(jsonBytes)
	time.Sleep(100 * time.Millisecond)
}

func TestDaemonReal(t *testing.T) {
	defer goleak.VerifyNone(t)
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

	conn.Write([]byte(authToken))

	timestampStr := "1234567890123456789"
	conn.Write([]byte(timestampStr))

	buf := make([]byte, 1)
	conn.Read(buf)

	file, err := os.CreateTemp("", "test_daemon_file_*.txt")
	if err == nil {
		file.WriteString("data")
		file.Close()
		hash, _ := momo_common.HashFile(file.Name())
		conn.Write([]byte(padTestString(hash, 64)))
		conn.Write([]byte(padTestString("test.txt", momo_common.FileInfoLength)))
		conn.Write([]byte(padTestString("4", momo_common.FileInfoLength)))

		conn.Write([]byte("data"))

		ackBuf := make([]byte, 4)
		conn.Read(ackBuf)
	}
}
