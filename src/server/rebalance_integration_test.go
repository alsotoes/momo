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

// TestRebalanceJoinMigrationEndToEnd proves the membership path over a real
// socket: a blob whose placement moved to the joining peer is pushed, stored,
// acknowledged, and only then dropped locally.
func TestRebalanceJoinMigrationEndToEnd(t *testing.T) {
	auth := strings.Repeat("b2c3d4e5", 8) // 64 chars, notsecret

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

	sender, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("sender store: %v", err)
	}
	t.Cleanup(func() { _ = sender.Close() })

	cfg := common.Configuration{}
	cfg.Global.AuthToken = auth
	cfg.Global.Protocol = "momo-tcp"
	cfg.Daemons = []*common.Daemon{
		{Host: "127.0.0.1:1", Data: t.TempDir()},
		{Host: listener.Addr().String(), Data: t.TempDir()},
		{Host: "127.0.0.1:2", Data: t.TempDir(), FailureDomain: "b"},
	}
	src := NewDaemonRebuildSource(0, cfg.Daemons, cfg, 2)
	src.SetStore(sender)

	hash, payload := findMigratableBlob(t, src.cmap, 0, 1)

	if err := sender.Put("migrate-me", hash, int64(len(payload)), "", strings.NewReader(string(payload))); err != nil {
		t.Fatalf("put: %v", err)
	}

	r := &rebalanceRunner{store: sender, pusher: src, cmap: src.cmap, self: 0, rf: 2,
		cfg: common.ConfigurationRebalance{Enabled: true, Interval: 60, DiskHighWater: 85, DiskLowWater: 70, MaxShedPerCycle: 8}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.onPeerJoin(ctx, 1); err != nil {
		t.Fatalf("join migration: %v", err)
	}

	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatalf("peer side: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("peer did not finish")
	}

	if _, err := receiver.GetMeta(hash); err != nil {
		t.Fatalf("peer did not store the migrated blob: %v", err)
	}
	blobs, err := sender.ListBlobs()
	if err != nil {
		t.Fatalf("sender ListBlobs: %v", err)
	}
	if len(blobs) != 0 {
		t.Fatalf("sender must drop the local copy after the ack, %d left", len(blobs))
	}
}

// findMigratableBlob returns a payload whose placement includes peer and not
// self, so the join path must move it.
func findMigratableBlob(t *testing.T, cmap *common.ClusterMap, self, peer int) (string, []byte) {
	t.Helper()
	for i := 0; i < 20000; i++ {
		p := []byte(strings.Repeat("j", 8) + string(rune('a'+i%26)) + strings.Repeat("x", i%7))
		h := common.HashBytes(p)
		peerOwns, selfOwns, err := placementOwnership(cmap, h, 2, self, peer)
		if err != nil {
			t.Fatalf("placement: %v", err)
		}
		if peerOwns && !selfOwns {
			return h, p
		}
	}
	t.Fatal("no placement probe found")
	return "", nil
}
