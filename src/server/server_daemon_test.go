package server

import (
	"context"
	"encoding/json"
	"fmt"
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

// freeAddrListener binds an ephemeral loopback port and returns the still-open
// listener, reserving its address. Unlike freeAddr, the port is never released,
// closing the TOCTOU window where another process could rebind it between the
// free and the caller's own net.Listen (see #846).
func freeAddrListener(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free listener: %v", err)
	}
	return l
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

	addr0 := freeAddr(t) // bound by ChangeReplicationModeServer below

	// Pre-bind the peer change-replication ports with open listeners so the
	// address is never released between selection and use (eliminates #846's
	// TOCTOU port-rebind race).
	l1 := freeAddrListener(t)
	defer l1.Close()
	addr1 := l1.Addr().String()
	go acceptReplication(l1)

	l2 := freeAddrListener(t)
	defer l2.Close()
	addr2 := l2.Addr().String()
	go acceptReplication(l2)

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
	if _, err := comm.HandshakeClient(authToken, time.Now().UnixNano(), 0); err != nil {
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

// daemonTestAuthToken is the shared cluster token for the daemon tests.
// notsecret
const daemonTestAuthToken = "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"

// startDaemonForForwardTest boots a real Chain-mode Daemon and dials it with a
// fresh client communicator. It returns the communicator and a cleanup to stop
// the daemon.
func startDaemonForForwardTest(t *testing.T) (*transport.MomoTCPCommunicator, func()) {
	t.Helper()
	comm, _, cleanup := startReplicationDaemon(t, common.ReplicationChain, 1)
	return comm, cleanup
}

// startDaemonForForwardTestQuorum is startDaemonForForwardTest with an explicit
// [global] write_quorum, so quorum enforcement can be exercised end to end.
func startDaemonForForwardTestQuorum(t *testing.T, writeQuorum int) (*transport.MomoTCPCommunicator, func()) {
	t.Helper()
	comm, _, cleanup := startReplicationDaemon(t, common.ReplicationChain, writeQuorum)
	return comm, cleanup
}

// startReplicationDaemon boots a real Daemon in the given replication mode with
// an explicit [global] write_quorum, and returns a client communicator dialled
// to it plus the primary address for a second connection.
func startReplicationDaemon(t *testing.T, mode int, writeQuorum int) (*transport.MomoTCPCommunicator, string, func()) {
	t.Helper()
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

	cfg := common.Configuration{
		Daemons: daemons,
		Global: common.ConfigurationGlobal{
			AuthToken:         daemonTestAuthToken,
			Protocol:          "momo-tcp",
			ReplicationFactor: 3,
			WriteQuorum:       writeQuorum,
		},
	}

	SetReplicationState(mode, time.Now().UnixNano())

	ctx, cancel := context.WithCancel(context.Background())
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
		cancel()
		t.Fatalf("Failed to dial primary Daemon: %v", err)
	}

	comm := transport.NewMomoTCPCommunicator(conn)
	if _, err := comm.HandshakeClient(daemonTestAuthToken, time.Now().UnixNano(), 0); err != nil {
		cancel()
		conn.Close()
		t.Fatalf("Handshake failed: %v", err)
	}

	return comm, primaryAddr, func() {
		comm.Close()
		cancel()
	}
}

// dialQuorumClient opens a second authenticated connection to a running test
// daemon, so a test can prove the daemon still serves after an earlier failure.
func dialQuorumClient(t *testing.T, addr string) *transport.MomoTCPCommunicator {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to dial Daemon: %v", err)
	}
	comm := transport.NewMomoTCPCommunicator(conn)
	if _, err := comm.HandshakeClient(daemonTestAuthToken, time.Now().UnixNano(), 0); err != nil {
		_ = conn.Close()
		t.Fatalf("Handshake failed: %v", err)
	}
	return comm
}

// contentWithPrimaryZero returns a payload whose CRUSH placement puts this node
// (ID 0) first, the only case where a direct-client upload forwards to peers.
// Placement depends on node ID/Weight/Domain only, so the daemon hosts are
// irrelevant; the bounded search fails loudly if placement semantics change.
func contentWithPrimaryZero(t *testing.T, prefix string) string {
	t.Helper()
	nodes := make([]*common.Node, 3)
	for i := range nodes {
		nodes[i] = &common.Node{ID: i, Weight: 1}
	}
	cmap := &common.ClusterMap{Nodes: nodes}
	for i := 0; i < 1000; i++ {
		payload := fmt.Sprintf("%s-%d", prefix, i)
		placement, err := cmap.Placement(common.HashBytes([]byte(payload)), 3)
		if err != nil {
			t.Fatalf("Placement: %v", err)
		}
		if len(placement) == 3 && placement[0].ID == 0 {
			return payload
		}
	}
	t.Fatalf("no payload found with node 0 as primary replica")
	return ""
}

// uploadToForwardTest sends metadata + payload over the supplied communicator.
func uploadToForwardTest(t *testing.T, comm *transport.MomoTCPCommunicator, name, content string) {
	t.Helper()
	meta := &common.FileMetadata{
		Name: name,
		Hash: common.HashBytes([]byte(content)),
		Size: int64(len(content)),
	}
	status, err := comm.SendMetadata(meta)
	if err != nil {
		t.Fatalf("Failed to send metadata: %v", err)
	}
	if status != transport.MetadataStatusSendPayload {
		t.Fatalf("Expected status %d, got %d", transport.MetadataStatusSendPayload, status)
	}
	if _, err := comm.Write([]byte(content)); err != nil {
		t.Fatalf("Failed to write payload: %v", err)
	}
}

// TestDaemonReplicationForwardFailure verifies that when replication forwarding
// to a peer fails, the server does NOT send a success ACK to the client (Rule 9:
// no silent data loss). Regression test for issue #627.
func TestDaemonReplicationForwardFailure(t *testing.T) {
	defer goleak.VerifyNone(t)

	comm, cleanup := startDaemonForForwardTest(t)
	defer cleanup()

	originalForward := connectToPeerStream.Load()
	failFn := forwardStreamFunc(func(cfg common.Configuration, content io.Reader, name, hash string, size int64, remotePath string, s3Headers map[string]string, serverId int, timestamp int64, requestedMode int, replicationFactor int) error {
		return net.ErrClosed
	})
	connectToPeerStream.Store(&failFn)
	defer func() { connectToPeerStream.Store(originalForward) }()

	uploadToForwardTest(t, comm, "forward-fail.txt", "forward-fail-data")

	comm.SetAbsoluteDeadline(time.Now().Add(3 * time.Second))
	if err := comm.ReceiveACK(); err == nil {
		t.Fatalf("Expected ReceiveACK to fail when replication forwarding failed, but it succeeded")
	}
}

// TestDaemonReplicationForwardTimeout verifies that a hung replication
// forwarder does not block the connection handler forever: after the bounded
// wait deadline the server abandons forwarding, suppresses the success ACK, and
// the handler returns. Regression test for issue #628.
func TestDaemonReplicationForwardTimeout(t *testing.T) {
	defer goleak.VerifyNone(t)

	comm, cleanup := startDaemonForForwardTest(t)
	defer cleanup()

	blocked := make(chan struct{})
	originalForward := connectToPeerStream.Load()
	hangFn := forwardStreamFunc(func(cfg common.Configuration, content io.Reader, name, hash string, size int64, remotePath string, s3Headers map[string]string, serverId int, timestamp int64, requestedMode int, replicationFactor int) error {
		<-blocked
		return nil
	})
	connectToPeerStream.Store(&hangFn)
	defer func() { connectToPeerStream.Store(originalForward) }()

	originalTimeout := forwardTimeout()
	replicationForwardTimeout.Store(int64(2 * time.Second))
	defer func() { replicationForwardTimeout.Store(int64(originalTimeout)) }()

	uploadToForwardTest(t, comm, "forward-hang.txt", "forward-hang-data")

	// The blocked forwarder means no success ACK is ever sent. ReceiveACK must
	// fail (connection closed after abandonment), proving the handler unblocked.
	done := make(chan error, 1)
	go func() {
		comm.SetAbsoluteDeadline(time.Now().Add(8 * time.Second))
		done <- comm.ReceiveACK()
	}()
	select {
	case ackErr := <-done:
		if ackErr == nil {
			t.Fatalf("Expected ReceiveACK to fail when forwarding timed out, but it succeeded")
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Handler blocked forever waiting for a hung forwarder")
	}

	// Unblock the forwarder so the daemon's forwarding goroutines (and the
	// waitForForwarders waiter) can drain before the test ends and goleak runs.
	close(blocked)
}

// stubForwarder installs a server-side forwarder that drains the streamed
// payload and returns want, so quorum tests control ACK counts without a peer.
func stubForwarder(t *testing.T, want error) {
	t.Helper()
	original := connectToPeerStream.Load()
	fn := forwardStreamFunc(func(cfg common.Configuration, content io.Reader, name, hash string, size int64, remotePath string, s3Headers map[string]string, serverId int, timestamp int64, requestedMode int, replicationFactor int) error {
		_, _ = io.Copy(io.Discard, content)
		return want
	})
	connectToPeerStream.Store(&fn)
	t.Cleanup(func() { connectToPeerStream.Store(original) })
}

// receiveACKWithDeadline reads the server ACK under a bounded deadline so a
// missing ACK surfaces as an error instead of hanging the test.
func receiveACKWithDeadline(t *testing.T, comm *transport.MomoTCPCommunicator) error {
	t.Helper()
	if err := comm.SetAbsoluteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetAbsoluteDeadline: %v", err)
	}
	return comm.ReceiveACK()
}

// TestDaemonWriteQuorumDefaultStillAcks proves the default (quorum 1) path is
// unchanged: the local store alone satisfies the quorum, so the client is acked
// regardless of whether this node was selected to forward.
func TestDaemonWriteQuorumDefaultStillAcks(t *testing.T) {
	defer goleak.VerifyNone(t)

	comm, cleanup := startDaemonForForwardTestQuorum(t, 1)
	defer cleanup()
	stubForwarder(t, nil)

	uploadToForwardTest(t, comm, "quorum-default.txt", "quorum-default-data")

	if err := receiveACKWithDeadline(t, comm); err != nil {
		t.Fatalf("Expected ACK with the default write_quorum, got %v", err)
	}
}

// TestDaemonWriteQuorumUnmetWithholdsACK proves the negative case: chain mode
// can acknowledge at most one forwarded replica, so write_quorum = 3 can never
// be met and the server must withhold the ACK instead of silently promising
// durability it did not achieve (#1323).
func TestDaemonWriteQuorumUnmetWithholdsACK(t *testing.T) {
	defer goleak.VerifyNone(t)

	comm, cleanup := startDaemonForForwardTestQuorum(t, 3)
	defer cleanup()
	stubForwarder(t, nil)

	uploadToForwardTest(t, comm, "quorum-unmet.txt", "quorum-unmet-data")

	if err := receiveACKWithDeadline(t, comm); err == nil {
		t.Fatalf("Expected no ACK when write_quorum cannot be met, but it succeeded")
	}
}

// TestDaemonWriteQuorumMetWithForwardedAck proves a quorum above 1 is
// satisfiable: the local store plus one forwarded peer ACK reaches quorum 2.
// Without this, a regression that always withholds for quorum > 1 would pass.
func TestDaemonWriteQuorumMetWithForwardedAck(t *testing.T) {
	defer goleak.VerifyNone(t)

	comm, cleanup := startDaemonForForwardTestQuorum(t, 2)
	defer cleanup()
	stubForwarder(t, nil)

	content := contentWithPrimaryZero(t, "quorum-met")
	uploadToForwardTest(t, comm, "quorum-met.txt", content)

	if err := receiveACKWithDeadline(t, comm); err != nil {
		t.Fatalf("Expected ACK when local + forwarded ack meets quorum 2, got %v", err)
	}
}

// TestDaemonWriteQuorumSplayCountsAllAcks proves splay counts every peer ACK:
// with this node primary, both other replicas are forwarded to, so quorum 3 is
// met (local + 2).
func TestDaemonWriteQuorumSplayCountsAllAcks(t *testing.T) {
	defer goleak.VerifyNone(t)

	comm, _, cleanup := startReplicationDaemon(t, common.ReplicationSplay, 3)
	defer cleanup()
	stubForwarder(t, nil)

	content := contentWithPrimaryZero(t, "quorum-splay")
	uploadToForwardTest(t, comm, "quorum-splay.txt", content)

	if err := receiveACKWithDeadline(t, comm); err != nil {
		t.Fatalf("Expected ACK when splay collects quorum 3 of 3 acks, got %v", err)
	}
}

// TestDaemonForwarderPanicWithholdsACKAndKeepsServing proves the Rule 37 guard
// in replicateToPeer: a panicking forwarder becomes an error (no ACK, no dead
// connection handler), and the daemon still serves the next upload.
func TestDaemonForwarderPanicWithholdsACKAndKeepsServing(t *testing.T) {
	defer goleak.VerifyNone(t)

	comm, addr, cleanup := startReplicationDaemon(t, common.ReplicationChain, 1)
	defer cleanup()

	originalForward := connectToPeerStream.Load()
	t.Cleanup(func() { connectToPeerStream.Store(originalForward) })
	panicFn := forwardStreamFunc(func(cfg common.Configuration, content io.Reader, name, hash string, size int64, remotePath string, s3Headers map[string]string, serverId int, timestamp int64, requestedMode int, replicationFactor int) error {
		panic("forwarder boom")
	})
	connectToPeerStream.Store(&panicFn)

	content := contentWithPrimaryZero(t, "quorum-panic")
	uploadToForwardTest(t, comm, "quorum-panic.txt", content)
	if err := receiveACKWithDeadline(t, comm); err == nil {
		t.Fatalf("Expected no ACK when the forwarder panics, but it succeeded")
	}

	// The daemon must still be alive: install a working forwarder and upload
	// over a fresh connection to the same node.
	okFn := forwardStreamFunc(func(cfg common.Configuration, content io.Reader, name, hash string, size int64, remotePath string, s3Headers map[string]string, serverId int, timestamp int64, requestedMode int, replicationFactor int) error {
		_, _ = io.Copy(io.Discard, content)
		return nil
	})
	connectToPeerStream.Store(&okFn)
	second := dialQuorumClient(t, addr)
	defer func() { _ = second.Close() }()

	secondContent := contentWithPrimaryZero(t, "quorum-panic-recovery")
	uploadToForwardTest(t, second, "quorum-panic-recovery.txt", secondContent)
	if err := receiveACKWithDeadline(t, second); err != nil {
		t.Fatalf("Daemon stopped serving after a forwarder panic: %v", err)
	}
}
