package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
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
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in acceptReplication: %v", r)
		}
	}()
	conn, err := l.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	authBuf := make([]byte, common.AuthTokenLength+common.TimestampLength+1)
	if _, err := io.ReadFull(conn, authBuf); err != nil {
		return
	}
	if _, err := conn.Write([]byte("0")); err != nil {
		return
	}
	buf := make([]byte, 1024)
	conn.Read(buf)
	conn.Write([]byte("OK"))
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

// TestDaemonReplicationForwardTimeout verifies that a hung replication
// forwarder does not block the connection handler forever: after the bounded
// wait deadline the server abandons forwarding, suppresses the success ACK, and
// the handler returns. Regression test for issue #628.
func TestDaemonReplicationForwardTimeout(t *testing.T) {
	defer goleak.VerifyNone(t)

	tempDir := t.TempDir()
	primaryAddr := freeAddr(t)
	daemons := []*common.Daemon{
		{Host: primaryAddr, Data: tempDir + "/000"},
		{Host: freeAddr(t), Data: tempDir + "/001"},
		{Host: freeAddr(t), Data: tempDir + "/002"},
	}
	for _, d := range daemons {
		os.MkdirAll(d.Data, 0755)
	}

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	cfg := common.Configuration{
		Daemons: daemons,
		Global: common.ConfigurationGlobal{
			AuthToken:         authToken,
			Protocol:          "momo-tcp",
			ReplicationFactor: 3,
		},
	}

	SetReplicationState(common.ReplicationChain, time.Now().UnixNano())

	// Override the forwarder to block forever, simulating a hung peer/S3 store.
	blocked := make(chan struct{})
	originalForward := connectToPeerStream
	connectToPeerStream = func(cfg common.Configuration, content io.Reader, name, hash string, size int64, remotePath string, s3Headers map[string]string, serverId int, timestamp int64, requestedMode int, replicationFactor int) {
		<-blocked
	}
	defer func() { connectToPeerStream = originalForward }()

	// Shorten the forward wait deadline so the test completes quickly.
	originalTimeout := replicationForwardTimeout
	replicationForwardTimeout = 500 * time.Millisecond
	defer func() { replicationForwardTimeout = originalTimeout }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Daemon(ctx, cfg, 0)

	var conn net.Conn
	var err error
	for i := 0; i < 20; i++ {
		conn, err = net.Dial("tcp", primaryAddr)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Failed to dial primary Daemon: %v", err)
	}
	defer conn.Close()

	comm := transport.NewMomoTCPCommunicator(conn)
	if _, err := comm.HandshakeClient(authToken, common.DummyEpoch, 0); err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	meta := &common.FileMetadata{
		Name: "forward-hang.txt",
		Hash: common.HashBytes([]byte("forward-hang-data")),
		Size: 16,
	}
	status, err := comm.SendMetadata(meta)
	if err != nil {
		t.Fatalf("Failed to send metadata: %v", err)
	}
	if status != transport.MetadataStatusSendPayload {
		t.Fatalf("Expected status %d, got %d", transport.MetadataStatusSendPayload, status)
	}
	if _, err := comm.Write([]byte("forward-hang-data")); err != nil {
		t.Fatalf("Failed to write payload: %v", err)
	}

	// The blocked forwarder means no success ACK is ever sent. ReceiveACK must
	// fail (connection closed after abandonment), proving the handler unblocked.
	done := make(chan error, 1)
	go func() {
		comm.SetAbsoluteDeadline(time.Now().Add(5 * time.Second))
		done <- comm.ReceiveACK()
	}()
	select {
	case ackErr := <-done:
		if ackErr == nil {
			t.Fatalf("Expected ReceiveACK to fail when forwarding timed out, but it succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Handler blocked forever waiting for a hung forwarder")
	}
}
