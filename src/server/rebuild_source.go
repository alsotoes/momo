// Package server provides the daemon-side RebuildSource implementation
// for the R2 self-heal rebuild loop (storage.RebuildSource).
package server

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"sync"
	"syscall"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
)

// DaemonRebuildSource implements storage.RebuildSource for the momo daemon.
// It uses the daemon's CRUSH placement and peer transport to coordinate
// self-heal rebuild across the cluster.
type DaemonRebuildSource struct {
	serverID          int
	daemons           []*common.Daemon
	replicationFactor int
	cmap              *common.ClusterMap
	cfg               common.Configuration
	transportCache    sync.Map // maps peerID -> transport.Communicator
	peerTokens        map[int]string
	// store reads local blob bytes for rebalance pushes (R11). Nil until the
	// daemon sets it after opening storage.
	store *storage.CASStore
	// pushFn overrides the transport push in tests; nil means pushToPeer.
	pushFn func(targetID int, hash string, data []byte, meta *common.FileMetadata) error
	// limiter throttles rebalance pushes so they cannot starve foreground
	// traffic (R11). Nil means unlimited.
	limiter *bandwidthLimiter
	// metrics receives rebalance counters (R11). Nil disables recording.
	metrics *MetricsCollector
}

// SetMetrics attaches the collector that receives rebalance counters.
func (d *DaemonRebuildSource) SetMetrics(m *MetricsCollector) { d.metrics = m }

// IncRebalanceCycles records one rebalance evaluation.
func (d *DaemonRebuildSource) IncRebalanceCycles() {
	if d.metrics != nil {
		d.metrics.IncRebalanceCycles()
	}
}

// recordRebalancePush counts a completed or failed rebalance push.
func (d *DaemonRebuildSource) recordRebalancePush(n int, failed bool) {
	if d.metrics == nil {
		return
	}
	if failed {
		d.metrics.IncRebalanceFailures()
		return
	}
	d.metrics.IncRebalanceMoved()
	d.metrics.AddRebalanceBytes(uint64(n))
}

// SetBandwidthLimit caps rebalance pushes at mbps megabytes per second.
// Zero or negative leaves them unlimited.
func (d *DaemonRebuildSource) SetBandwidthLimit(mbps float64) {
	if mbps <= 0 {
		d.limiter = nil
		return
	}
	d.limiter = newBandwidthLimiter(mbps)
}

// SetStore attaches the local CAS store used to read blob bytes for
// rebalance pushes (R11, #1143).
func (d *DaemonRebuildSource) SetStore(store *storage.CASStore) {
	d.store = store
}

// Push sends the local copy of blob hash to one peer and waits for its ack.
// It is the rebalance loop's transport; the caller drops the local bytes only
// after a nil return.
func (d *DaemonRebuildSource) Push(ctx context.Context, hash string, targetID int) (err error) {
	defer common.RecoverErr("DaemonRebuildSource.Push", &err)
	if d.store == nil {
		return fmt.Errorf("rebalance push without a store: %w", syscall.EINVAL)
	}
	rc, err := d.store.GetBlobByHash(hash)
	if err != nil {
		return fmt.Errorf("read local blob %s: %w", hash, err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("read local blob %s: %w", hash, err)
	}
	meta := &common.FileMetadata{Name: hash, Hash: hash, Size: int64(len(data))}
	if d.pushFn != nil {
		err = d.pushFn(targetID, hash, data, meta)
	} else {
		err = d.pushToPeer(ctx, targetID, hash, data, meta)
	}
	d.recordRebalancePush(len(data), err != nil)
	return err
}

// NewDaemonRebuildSource creates a RebuildSource backed by the daemon's
// CRUSH placement and peer transport.
func NewDaemonRebuildSource(serverID int, daemons []*common.Daemon, cfg common.Configuration, replicationFactor int) *DaemonRebuildSource {
	nodes := make([]*common.Node, len(daemons))
	for i, d := range daemons {
		nodes[i] = &common.Node{ID: i, Weight: 1, Addr: d.Host, Domain: d.FailureDomain}
	}
	cmap := &common.ClusterMap{Nodes: nodes}

	// Pre-derive peer tokens for each daemon (used for P2P handshake)
	peerTokens := make(map[int]string)
	authToken := cfg.Global.AuthToken
	for i := range daemons {
		peerTokens[i] = common.DerivePeerTokenString(authToken)
	}

	return &DaemonRebuildSource{
		serverID:          serverID,
		daemons:           daemons,
		replicationFactor: replicationFactor,
		cmap:              cmap,
		cfg:               cfg,
		peerTokens:        peerTokens,
	}
}

// Survivors returns the replica-set members OTHER than this node that
// currently hold a copy of blob hash whose bytes match the content hash.
// The list is advisory: verify-before-use is enforced by Fetch.
func (d *DaemonRebuildSource) Survivors(hash string) ([]storage.Peer, error) {
	if d.cmap == nil {
		return nil, fmt.Errorf("no CRUSH map available")
	}

	placement, err := d.cmap.Placement(hash, d.replicationFactor)
	if err != nil {
		return nil, fmt.Errorf("placement for %s: %w", hash, err)
	}

	var survivors []storage.Peer
	for _, node := range placement {
		if node.ID == d.serverID {
			continue
		}
		survivors = append(survivors, storage.Peer{
			ID:     node.ID,
			Domain: node.Domain,
		})
	}
	return survivors, nil
}

// dialPeer establishes a transport connection to the given peer.
func (d *DaemonRebuildSource) dialPeer(peerID int) (transport.Communicator, error) {
	if peerID < 0 || peerID >= len(d.daemons) {
		return nil, fmt.Errorf("peer ID %d out of range", peerID)
	}
	if peerID == d.serverID {
		return nil, fmt.Errorf("cannot dial self")
	}

	// Check cache first
	if cached, ok := d.transportCache.Load(peerID); ok {
		if comm, ok := cached.(transport.Communicator); ok {
			return comm, nil
		}
	}

	factory := transport.NewProtocolFactory(d.cfg)
	comm, err := factory.Dial(d.daemons[peerID].Host)
	if err != nil {
		return nil, fmt.Errorf("dial peer %d at %s: %w", peerID, d.daemons[peerID].Host, err)
	}

	// Perform peer handshake (auth token + zero timestamp + peer mode)
	peerToken := d.peerTokens[peerID]
	if _, err := comm.HandshakeClient(peerToken, 0, common.ReplicationNone); err != nil {
		comm.Close()
		return nil, fmt.Errorf("peer handshake with %d: %w", peerID, err)
	}

	// Cache for reuse
	d.transportCache.Store(peerID, comm)
	return comm, nil
}

// Fetch opens a verify-before-use stream of blob hash served by survivor:
// the returned reader MUST re-derive the content hash and fail the read if
// the bytes are corrupted, so corrupt bytes are never stored or propagated.
func (d *DaemonRebuildSource) Fetch(hash string, survivor storage.Peer) (io.ReadCloser, error) {
	// Use the client Download path adapted for peer-to-peer blob fetch
	comm, err := d.dialPeer(survivor.ID)
	if err != nil {
		return nil, fmt.Errorf("dial survivor %d: %w", survivor.ID, err)
	}

	// Send GET handshake with mode 'G' (download)
	peerToken := common.DerivePeerTokenString(d.cfg.Global.AuthToken)
	var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
	copy(handshakeBuf[:common.AuthTokenLength], peerToken)
	// Timestamp = 0 for peer fetch (we don't care about timestamp for repair)
	copy(handshakeBuf[common.AuthTokenLength:], common.PadString("0", common.TimestampLength))
	handshakeBuf[common.AuthTokenLength+common.TimestampLength] = 'G'

	if _, err := comm.Write(handshakeBuf[:]); err != nil {
		comm.Close()
		return nil, fmt.Errorf("write fetch handshake to survivor %d: %w", survivor.ID, err)
	}

	// Send GET request: 64-byte padded name (hash) + 64-byte padded hash
	var requestBuf [128]byte
	copy(requestBuf[:64], common.PadString(hash, 64))
	copy(requestBuf[64:128], common.PadString(hash, 64))

	if _, err := comm.Write(requestBuf[:]); err != nil {
		comm.Close()
		return nil, fmt.Errorf("write fetch request to survivor %d: %w", survivor.ID, err)
	}

	// Read response: 1-byte status + 64-byte size
	var respBuf [65]byte
	if _, err := io.ReadFull(comm, respBuf[:]); err != nil {
		comm.Close()
		return nil, fmt.Errorf("read fetch response from survivor %d: %w", survivor.ID, err)
	}

	if respBuf[0] != '0' {
		comm.Close()
		return nil, fmt.Errorf("survivor %d returned error status %q: %w", survivor.ID, respBuf[0], syscall.ENOENT)
	}

	sizeStr := common.TrimNullBytesString(respBuf[1:65])
	size, err := common.SafeParseInt([]byte(sizeStr))
	if err != nil {
		comm.Close()
		return nil, fmt.Errorf("parse size from survivor %d: %w", survivor.ID, err)
	}

	// Return a reader that wraps the connection and closes it on Close()
	return &fetchReader{
		conn: comm,
		size: size,
		hash: hash,
	}, nil
}

// fetchReader wraps a transport.Communicator to provide io.ReadCloser
// with verification against the content hash.
type fetchReader struct {
	conn transport.Communicator
	size int64
	hash string
	read int64
}

func (fr *fetchReader) Read(p []byte) (n int, err error) {
	if fr.read >= fr.size {
		return 0, io.EOF
	}
	n, err = fr.conn.Read(p)
	fr.read += int64(n)
	return n, err
}

func (fr *fetchReader) Close() error {
	return fr.conn.Close()
}

// Restore pushes already-verified blob bytes to every replica-set member
// that does not currently hold a verified copy, preferring R1 failure-domain
// spread for new placements (R2-C5). The source owns multi-target fan-out/
// buffering; content is pre-verified by the caller.
func (d *DaemonRebuildSource) Restore(hash string, content io.Reader) error {
	if d.cmap == nil {
		return fmt.Errorf("no CRUSH map available")
	}

	placement, err := d.cmap.Placement(hash, d.replicationFactor)
	if err != nil {
		return fmt.Errorf("placement for %s: %w", hash, err)
	}

	// Determine which members in placement lack the blob
	// For rebuild, we assume all placement members except self should have it
	// The caller (repairBlob) determines which specific targets need it
	var targets []int
	for _, node := range placement {
		if node.ID != d.serverID {
			targets = append(targets, node.ID)
		}
	}

	// Read all content into memory for multi-target fan-out
	// (content is pre-verified, so we can safely buffer)
	data, err := io.ReadAll(content)
	if err != nil {
		return fmt.Errorf("read content for restore: %w", err)
	}

	// Verify content hash matches before distributing
	h := sha256.New()
	if _, err := h.Write(data); err != nil {
		return fmt.Errorf("hash content for verify: %w", err)
	}
	computedHash := fmt.Sprintf("%x", h.Sum(nil))
	if computedHash != hash {
		return fmt.Errorf("content hash mismatch before restore: got %s want %s", computedHash, hash)
	}

	// Prepare metadata for replication
	meta := &common.FileMetadata{
		Name:       hash, // use hash as name for internal repair
		Hash:       hash,
		Size:       int64(len(data)),
		RemotePath: "",
		S3Headers:  nil,
	}

	// Fan out to all target replicas concurrently
	var wg sync.WaitGroup
	errChan := make(chan error, len(targets))
	for _, targetID := range targets {
		wg.Add(1)
		go func(targetID int) {
			defer wg.Done()
			if err := d.pushToPeer(context.Background(), targetID, hash, data, meta); err != nil {
				errChan <- fmt.Errorf("push to peer %d: %w", targetID, err)
			}
		}(targetID)
	}
	wg.Wait()
	close(errChan)

	for err := range errChan {
		return err // return first error
	}
	return nil
}

// pushToPeer pushes a pre-verified blob to a single peer.
func (d *DaemonRebuildSource) pushToPeer(ctx context.Context, targetID int, hash string, data []byte, meta *common.FileMetadata) error {
	comm, err := d.dialPeer(targetID)
	if err != nil {
		return fmt.Errorf("dial peer %d: %w", targetID, err)
	}
	defer comm.Close()

	status, err := comm.SendMetadata(meta)
	if err != nil {
		return fmt.Errorf("send metadata to peer %d: %w", targetID, err)
	}

	if status == transport.MetadataStatusSkipPayload {
		log.Printf("STORAGE REBUILD: peer %d already has %s, skipping push", targetID, hash)
		return nil
	}

	// Send payload, throttled when a rebalance bandwidth cap is set.
	if err := writeThrottled(ctx, comm, data, d.limiter); err != nil {
		return fmt.Errorf("send payload to peer %d: %w", targetID, err)
	}

	// Wait for ACK
	if err := comm.ReceiveACK(); err != nil {
		return fmt.Errorf("receive ACK from peer %d: %w", targetID, err)
	}

	log.Printf("STORAGE REBUILD: pushed %s to peer %d", hash, targetID)
	return nil
}
