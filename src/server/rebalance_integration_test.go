package server

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
)

// TestRebalancePushEndToEnd drives a rebalance push over a real TCP socket: the
// real dial, the real handshake (including its anti-replay timestamp), metadata
// negotiation, a streamed payload, storage on the peer, and the ACK the sender
// waits for. This is the path that a unit test with a mocked transport cannot
// cover.
func TestRebalancePushEndToEnd(t *testing.T) {
	auth := strings.Repeat("a1b2c3d4", 8) // 64 chars, notsecret

	receiver, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("receiver store: %v", err)
	}
	t.Cleanup(func() { _ = receiver.Close() })

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	serverErr := make(chan error, 1)
	go func() { serverErr <- serveOneRebalancePush(listener, receiver, auth) }()

	sender, hashes := rebalanceTestStore(t, 1)
	cfg := common.Configuration{}
	cfg.Global.AuthToken = auth
	cfg.Global.Protocol = "momo-tcp"
	cfg.Daemons = []*common.Daemon{
		{Host: "127.0.0.1:1", Data: t.TempDir()},
		{Host: listener.Addr().String(), Data: t.TempDir()},
	}

	src := NewDaemonRebuildSource(0, cfg.Daemons, cfg, 2)
	src.SetStore(sender)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := src.Push(ctx, hashes[0], 1); err != nil {
		t.Fatalf("push over the real transport: %v", err)
	}

	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatalf("peer side: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("peer did not finish")
	}

	meta, err := receiver.GetMeta(hashes[0])
	if err != nil {
		t.Fatalf("peer did not store the blob: %v", err)
	}
	if meta.Hash != hashes[0] {
		t.Fatalf("peer stored %q, want %q", meta.Hash, hashes[0])
	}
}

// serveOneRebalancePush accepts one peer connection and plays the daemon's
// receive side: handshake, mode, metadata, payload, ACK.
func serveOneRebalancePush(listener net.Listener, store storage.Store, auth string) (err error) {
	defer common.RecoverErr("serveOneRebalancePush", &err)
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	comm := transport.NewMomoTCPCommunicator(conn)
	comm.SetStore(store)
	if _, _, err := comm.HandshakeServer([]byte(common.PadString(auth, common.AuthTokenLength))); err != nil {
		return err
	}
	if err := comm.SendReplicationMode(common.ReplicationNone); err != nil {
		return err
	}
	meta, err := comm.ReceiveMetadata()
	if err != nil {
		return err
	}
	if err := comm.SendMetadataStatus(transport.MetadataStatusSendPayload); err != nil {
		return err
	}
	if err := getFile(comm, store, meta.Name, meta.Hash, meta.Size, ""); err != nil {
		return err
	}
	return comm.SendACK(1)
}
