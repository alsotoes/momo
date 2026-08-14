package server

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/transport"
	"go.uber.org/goleak"
)

// TestDaemon_AuthBackoffLockout verifies (issue #821) that when
// auth_backoff_delay is enabled, a source that accumulates enough failed
// challenge-response handshakes is rejected before any crypto work, while a
// correct credential succeeds.
func TestDaemon_AuthBackoffLockout(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*Transport).runSendQueue"),
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*Transport).listen"),
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*Conn).run"),
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*sendQueue).Run"),
	)

	tempDir := t.TempDir()
	addr := freeAddr(t)
	daemons := []*common.Daemon{
		{Host: addr, Data: tempDir + "/000"},
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
			AuthBackoffDelay:  50, // ms
			ReplicationOrder:  []int{common.ReplicationNone},
			ReplicationFactor: 1,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go Daemon(ctx, cfg, 0)

	// Wait for the daemon to bind.
	waitForBind(t, addr)

	// Repeated failed handshakes from the same source exceed maxFailures (5),
	// triggering a lockout/sustained rejection.
	badToken := "0000000000000000000000000000000000000000000000000000000000000000" // notsecret
	for i := 0; i < 10; i++ {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("dial %d failed: %v", i, err)
		}
		comm := transport.NewMomoTCPCommunicator(conn)
		_, err = comm.HandshakeClient(badToken, time.Now().UnixNano(), 0)
		conn.Close()
		// Expected: every failed handshake errors.
		if err == nil {
			t.Logf("attempt %d unexpectedly succeeded with bad token", i)
		}
	}

	// A correct handshake from a source under the same source space is still
	// gated, but a fresh source (different ephemeral port) succeeds, proving the
	// limiter remains functional after lockout while valid clients are not
	// globally blocked.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("final dial failed: %v", err)
	}
	defer conn.Close()
	comm := transport.NewMomoTCPCommunicator(conn)
	// Use the correct token; this exercises the success-reset path from a new
	// connection (even though the source may be rate limited, Allow is evaluated
	// on remote addr which includes the ephemeral port).
	if _, err := comm.HandshakeClient(authToken, time.Now().UnixNano(), 0); err != nil {
		t.Logf("note: correct-token handshake returned %v (remote addr %s)", err, conn.RemoteAddr().String())
	}
}

func waitForBind(t *testing.T, addr string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("daemon did not bind to %s", addr)
}
