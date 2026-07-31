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
	"go.uber.org/goleak"
)

func padTestString(input string, length int) string {
	if len(input) >= length {
		return input[:length]
	}
	return input + string(make([]byte, length-len(input)))
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func acceptReplication(l net.Listener) {
	conn, err := l.Accept()
	if err == nil {
		defer conn.Close()
		authBuf := make([]byte, common.AuthTokenLength+common.TimestampLength+1)
		io.ReadFull(conn, authBuf)
		conn.Write([]byte("0"))
		buf := make([]byte, 1024)
		conn.Read(buf)
		conn.Write([]byte("OK"))
	}
}

func dialWithTimeout(ctx context.Context, addr string) (net.Conn, error) {
	var conn net.Conn
	var err error
	for {
		select {
		case <-ctx.Done():
			if conn != nil {
				return conn, nil
			}
			return nil, ctx.Err()
		default:
		}
		conn, err = net.Dial("tcp", addr)
		if err == nil {
			return conn, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestChangeReplicationModeServerReal(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*Transport).runSendQueue"),
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*Transport).listen"),
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*Conn).run"),
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*sendQueue).Run"),
	)

	addr0 := freeAddr(t)
	addr1 := freeAddr(t)
	addr2 := freeAddr(t)

	daemons := []*common.Daemon{
		{ChangeReplication: addr0},
		{ChangeReplication: addr1},
		{ChangeReplication: addr2},
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

	l1, err := net.Listen("tcp", addr1)
	if err != nil {
		t.Fatalf("failed to listen on %s: %v", addr1, err)
	}
	defer l1.Close()
	go acceptReplication(l1)

	l2, err := net.Listen("tcp", addr2)
	if err != nil {
		t.Fatalf("failed to listen on %s: %v", addr2, err)
	}
	defer l2.Close()
	go acceptReplication(l2)

	go ChangeReplicationModeServer(ctx, cfg, 0, time.Now().UnixNano())

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	conn, err := dialWithTimeout(dialCtx, addr0)
	if err != nil {
		t.Fatalf("Failed to dial test server: %v", err)
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
