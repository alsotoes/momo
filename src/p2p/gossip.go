package p2p

import (
	"log"
	"sync"
	"syscall"
	"time"
)

// MaxPeersInHeartbeat is the upper bound on peers processed from a single
// heartbeat or membership RPC to prevent CPU exhaustion via malicious packets.
const MaxPeersInHeartbeat = 256

// GossipConfig holds configuration for the Gossiper.
type GossipConfig struct {
	LocalID           int32
	HeartbeatInterval time.Duration
	SuspicionTimeout  time.Duration
	Fanout            int
}

// DefaultGossipConfig returns sensible defaults for gossip.
func DefaultGossipConfig(localID int32) GossipConfig {
	return GossipConfig{
		LocalID:           localID,
		HeartbeatInterval: 1 * time.Second,
		SuspicionTimeout:  5 * time.Second,
		Fanout:            3,
	}
}

// Gossiper runs the gossip protocol on top of a Transport.
// It periodically sends heartbeats to random peers, merges membership
// information from received heartbeats, and marks unreachable peers as suspect/offline.
type Gossiper struct {
	cfg       GossipConfig
	transport Transport
	done      chan struct{}
	closed    bool
	mu        sync.Mutex
	wg        sync.WaitGroup

	onJoin  func(peer *Peer)
	onLeave func(peerID int32)

	scatterGather *ScatterGather
	leaseManager  *LeaseManager
}

// NewGossiper creates a new Gossiper.
func NewGossiper(cfg GossipConfig, transport Transport) *Gossiper {
	return &Gossiper{
		cfg:       cfg,
		transport: transport,
		done:      make(chan struct{}),
	}
}

// OnJoin sets a callback invoked when a new peer joins the cluster.
func (g *Gossiper) OnJoin(fn func(peer *Peer)) {
	g.onJoin = fn
}

// OnLeave sets a callback invoked when a peer leaves the cluster.
func (g *Gossiper) OnLeave(fn func(peerID int32)) {
	g.onLeave = fn
}

// SetScatterGather attaches a ScatterGather instance whose query RPCs will be
// routed by the gossiper's consumer loop.
func (g *Gossiper) SetScatterGather(sg *ScatterGather) {
	g.scatterGather = sg
}

// SetLeaseManager attaches a LeaseManager instance whose lease RPCs will be
// routed by the gossiper's consumer loop.
func (g *Gossiper) SetLeaseManager(lm *LeaseManager) {
	g.leaseManager = lm
}

// Start launches the heartbeat and suspicion loops.
func (g *Gossiper) Start() {
	g.wg.Add(2)
	go g.heartbeatLoop()
	go g.suspicionLoop()
}

// Stop signals the gossip loops to terminate and waits for them.
func (g *Gossiper) Stop() {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return
	}
	g.closed = true
	close(g.done)
	g.mu.Unlock()
	g.wg.Wait()
}

// heartbeatLoop periodically sends heartbeats to random peers.
func (g *Gossiper) heartbeatLoop() {
	defer g.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Gossip heartbeatLoop panic recovered: %v (errno=%d)", r, syscall.EIO)
		}
	}()

	ticker := time.NewTicker(g.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.done:
			return
		case <-ticker.C:
			g.sendHeartbeat()
		}
	}
}

// sendHeartbeat sends a heartbeat RPC with the current peer list to k random peers.
func (g *Gossiper) sendHeartbeat() {
	pm := g.transport.Peers()
	peers := pm.RandomPeers(g.cfg.Fanout, g.cfg.LocalID)
	if len(peers) == 0 {
		return
	}

	peerInfos := pm.PeerInfos()
	payload := &HeartbeatPayload{Peers: peerInfos}
	encoded := payload.Encode()

	rpc := &RPC{
		From:    g.cfg.LocalID,
		Type:    MsgHeartbeat,
		Payload: encoded,
	}

	for _, peer := range peers {
		if err := g.transport.Send(peer.ID, rpc); err != nil {
			log.Printf("Gossip heartbeat to peer %d failed: %v (errno=%d)", peer.ID, err, syscall.EHOSTUNREACH)
		}
	}
}

// suspicionLoop periodically checks for peers that haven't sent heartbeats
// within the suspicion timeout window and marks them suspect or offline.
func (g *Gossiper) suspicionLoop() {
	defer g.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Gossip suspicionLoop panic recovered: %v (errno=%d)", r, syscall.EIO)
		}
	}()

	ticker := time.NewTicker(g.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.done:
			return
		case <-ticker.C:
			g.checkSuspicion()
		}
	}
}

// checkSuspicion marks peers as suspect or offline based on last seen time.
func (g *Gossiper) checkSuspicion() {
	now := time.Now()
	peers := g.transport.Peers().All()

	for _, p := range peers {
		if p.ID == g.cfg.LocalID {
			continue
		}
		elapsed := now.Sub(p.LastSeen())
		switch p.State() {
		case PeerStateAlive:
			if elapsed > g.cfg.SuspicionTimeout {
				p.SetState(PeerStateSuspect)
				log.Printf("Gossip: peer %d marked SUSPECT (last seen %v ago)", p.ID, elapsed)
			}
		case PeerStateSuspect:
			if elapsed > 2*g.cfg.SuspicionTimeout {
				p.SetState(PeerStateOffline)
				log.Printf("Gossip: peer %d marked OFFLINE (last seen %v ago)", p.ID, elapsed)
				if g.onLeave != nil {
					g.onLeave(p.ID)
				}
			}
		}
	}
}

// HandleRPC processes an incoming gossip RPC. It should be called for each
// RPC received from the transport's Consume() channel.
func (g *Gossiper) HandleRPC(rpc *RPC) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Gossip HandleRPC panic recovered: %v (errno=%d)", r, syscall.EIO)
		}
	}()
	switch rpc.Type {
	case MsgHeartbeat:
		g.handleHeartbeat(rpc)
	case MsgMembership:
		g.handleMembership(rpc)
	case MsgSuspect:
		g.handleSuspect(rpc)
	case MsgQuery, MsgQueryResponse:
		if g.scatterGather != nil {
			g.scatterGather.HandleRPC(rpc)
		}
	case MsgLeaseRequest, MsgLeaseGrant, MsgLeaseRelease:
		if g.leaseManager != nil {
			g.leaseManager.HandleLeaseRPC(rpc)
		}
	}
}

// handleHeartbeat merges the peer list from the heartbeat into the local peer map.
func (g *Gossiper) handleHeartbeat(rpc *RPC) {
	payload, err := DecodeHeartbeatPayload(rpc.Payload)
	if err != nil {
		log.Printf("Gossip: failed to decode heartbeat from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	if len(payload.Peers) > MaxPeersInHeartbeat {
		log.Printf("Gossip: heartbeat from peer %d contains %d peers, truncating to %d (errno=%d)",
			rpc.From, len(payload.Peers), MaxPeersInHeartbeat, syscall.E2BIG)
		payload.Peers = payload.Peers[:MaxPeersInHeartbeat]
	}

	for _, pi := range payload.Peers {
		if pi.ID == g.cfg.LocalID {
			continue
		}
		if existing := g.transport.Peers().Get(pi.ID); existing == nil {
			peer := NewPeer(pi.ID, pi.Addr)
			g.transport.Peers().Add(peer)
			log.Printf("Gossip: discovered new peer %d at %s via heartbeat from %d", pi.ID, pi.Addr, rpc.From)
			if g.onJoin != nil {
				g.onJoin(peer)
			}
		}
	}
}

// handleMembership processes a membership update (new node join/leave announcement).
func (g *Gossiper) handleMembership(rpc *RPC) {
	payload, err := DecodeHeartbeatPayload(rpc.Payload)
	if err != nil {
		log.Printf("Gossip: failed to decode membership from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	if len(payload.Peers) > MaxPeersInHeartbeat {
		log.Printf("Gossip: membership from peer %d contains %d peers, truncating to %d (errno=%d)",
			rpc.From, len(payload.Peers), MaxPeersInHeartbeat, syscall.E2BIG)
		payload.Peers = payload.Peers[:MaxPeersInHeartbeat]
	}

	for _, pi := range payload.Peers {
		if pi.ID == g.cfg.LocalID {
			continue
		}
		if existing := g.transport.Peers().Get(pi.ID); existing == nil {
			peer := NewPeer(pi.ID, pi.Addr)
			g.transport.Peers().Add(peer)
			log.Printf("Gossip: new peer %d joined via membership update from %d", pi.ID, rpc.From)
			if g.onJoin != nil {
				g.onJoin(peer)
			}
		}
	}
}

// handleSuspect processes a suspicion announcement about a peer.
func (g *Gossiper) handleSuspect(rpc *RPC) {
	if len(rpc.Payload) < 4 {
		log.Printf("Gossip: suspect RPC from peer %d has short payload (len=%d, errno=%d)",
			rpc.From, len(rpc.Payload), syscall.EBADMSG)
		return
	}
	suspectID := int32(0)
	suspectID |= int32(rpc.Payload[0]) << 24
	suspectID |= int32(rpc.Payload[1]) << 16
	suspectID |= int32(rpc.Payload[2]) << 8
	suspectID |= int32(rpc.Payload[3])

	if suspectID == g.cfg.LocalID {
		return
	}

	if peer := g.transport.Peers().Get(suspectID); peer != nil {
		if peer.State() == PeerStateAlive {
			peer.SetState(PeerStateSuspect)
			log.Printf("Gossip: peer %d marked SUSPECT via announcement from %d", suspectID, rpc.From)
		}
	}
}

// AnnounceJoin sends a membership announcement to all peers about a newly joined node.
func (g *Gossiper) AnnounceJoin(newPeer *Peer) {
	payload := &HeartbeatPayload{
		Peers: []PeerInfo{{ID: newPeer.ID, Addr: newPeer.Addr}},
	}
	rpc := &RPC{
		From:    g.cfg.LocalID,
		Type:    MsgMembership,
		Payload: payload.Encode(),
	}
	g.transport.Broadcast(rpc)
}

// AnnounceSuspect sends a suspicion announcement about a peer to all other peers.
func (g *Gossiper) AnnounceSuspect(peerID int32) {
	payload := make([]byte, 4)
	payload[0] = byte(peerID >> 24)
	payload[1] = byte(peerID >> 16)
	payload[2] = byte(peerID >> 8)
	payload[3] = byte(peerID)

	rpc := &RPC{
		From:    g.cfg.LocalID,
		Type:    MsgSuspect,
		Payload: payload,
	}
	g.transport.Broadcast(rpc)
}

// Run starts the gossiper and processes incoming RPCs until Stop is called.
// This is a convenience method that combines Start() with a consume loop.
func (g *Gossiper) Run() {
	g.Start()

	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Gossip consumer loop panic recovered: %v (errno=%d)", r, syscall.EIO)
			}
		}()
		for {
			select {
			case <-g.done:
				return
			case rpc := <-g.transport.Consume():
				g.HandleRPC(&rpc)
			}
		}
	}()
}

// Close stops the gossiper. Alias for Stop() for io.Closer compatibility.
func (g *Gossiper) Close() error {
	g.Stop()
	return nil
}

// Ensure syscall is referenced for error wrapping.
var _ = syscall.EIO
