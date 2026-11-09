package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/transport"
	"go.uber.org/goleak"
)

// captureReplication answers a peer handshake and forwards the propagated mode.
func captureReplication(l net.Listener, got chan<- common.ReplicationData) {
	conn, err := l.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	// Read exactly the handshake frame; a single loose Read would swallow the
	// JSON payload that follows in the same or the next segment.
	buf := make([]byte, common.AuthTokenLength+common.TimestampLength+1)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return
	}
	if _, err := conn.Write([]byte("0")); err != nil {
		return
	}
	var data common.ReplicationData
	if err := json.NewDecoder(conn).Decode(&data); err != nil {
		return
	}
	select {
	case got <- data:
	default:
	}
	_, _ = conn.Write([]byte("OK"))
}

// TestNonPrimaryNodePropagatesModeChange proves #1144 Phase 3: a node other than
// node 0 applies a change and forwards it to the rest of the cluster.
func TestNonPrimaryNodePropagatesModeChange(t *testing.T) {
	defer goleak.VerifyNone(t)

	peerLn := freeAddrListener(t)
	defer func() { _ = peerLn.Close() }()
	got := make(chan common.ReplicationData, 1)
	go captureReplication(peerLn, got)

	selfLn := freeAddrListener(t)
	selfAddr := selfLn.Addr().String()
	_ = selfLn.Close() // ChangeReplicationModeServer binds it itself

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			AuthToken: "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6", // notsecret
			Protocol:  "momo-tcp",
		},
		Daemons: []*common.Daemon{
			{ChangeReplication: peerLn.Addr().String()},
			{ChangeReplication: selfAddr, Data: t.TempDir()},
		},
	}

	t.Cleanup(func() { SetReplicationState(common.ReplicationNone, 0) })

	ctx, cancel := context.WithCancel(context.Background())
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = ChangeReplicationModeServer(ctx, cfg, 1, time.Now().UnixNano())
	}()
	defer func() {
		cancel()
		<-serverDone
	}()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	conn, err := dialWithTimeout(dialCtx, selfAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	comm := transport.NewMomoTCPCommunicator(conn)
	// Deliberately different from the payload timestamp: the handler must use the
	// payload's logical clock, never the per-hop handshake time, or every hop would
	// look newer and re-propagate forever.
	handshakeTS := time.Now().UnixNano()
	ts := handshakeTS - 1_000_000
	if _, err := comm.HandshakeClient(cfg.Global.AuthToken, handshakeTS, 0); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	payload, err := json.Marshal(common.ReplicationData{New: common.ReplicationSplay, TimeStamp: ts})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := comm.Write(append(payload, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}
	ack := make([]byte, 2)
	if _, err := io.ReadFull(comm, ack); err != nil {
		t.Fatalf("ack: %v", err)
	}

	select {
	case data := <-got:
		if data.New != common.ReplicationSplay || data.TimeStamp != ts {
			t.Fatalf("propagated %+v, want mode %d ts %d", data, common.ReplicationSplay, ts)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("node 1 did not propagate the mode change to its peer")
	}
}

// TestStaleReplicationUpdateIsIgnored pins the loop guard: a duplicate or older
// timestamp is not applied and therefore not re-propagated.
func TestStaleReplicationUpdateIsIgnored(t *testing.T) {
	SetReplicationState(common.ReplicationNone, 0)
	state, applied := applyReplicationState(common.ReplicationChain, 2_000)
	if !applied || state.New != common.ReplicationChain {
		t.Fatalf("first update must apply: %+v applied=%v", state, applied)
	}
	state, applied = applyReplicationState(common.ReplicationSplay, 1_000)
	if applied || state.New != common.ReplicationChain {
		t.Fatalf("older update must be ignored: %+v applied=%v", state, applied)
	}
	state, applied = applyReplicationState(common.ReplicationSplay, 2_000)
	if applied || state.New != common.ReplicationChain {
		t.Fatalf("duplicate timestamp must be ignored: %+v applied=%v", state, applied)
	}
	state, applied = applyReplicationState(common.ReplicationSplay, 3_000)
	if !applied || state.New != common.ReplicationSplay {
		t.Fatalf("newer update must apply: %+v applied=%v", state, applied)
	}
}

func TestZeroTimestampKeepsLegacyBehaviour(t *testing.T) {
	SetReplicationState(common.ReplicationNone, 0) // reset the package-global state
	if _, applied := applyReplicationState(common.ReplicationChain, 5_000); !applied {
		t.Fatal("setup failed")
	}
	state, applied := applyReplicationState(common.ReplicationNone, 0)
	if !applied || state.New != common.ReplicationNone {
		t.Fatalf("manual (zero-ts) override must apply: %+v applied=%v", state, applied)
	}
}
