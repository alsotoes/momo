package p2p

import (
	"log"
	"sync"
	"sync/atomic"
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
	PingTimeout       time.Duration
	IndirectPingCount int
	RTTAlpha          float64
}

// DefaultGossipConfig returns sensible defaults for gossip.
func DefaultGossipConfig(localID int32) GossipConfig {
	return GossipConfig{
		LocalID:           localID,
		HeartbeatInterval: 1 * time.Second,
		SuspicionTimeout:  5 * time.Second,
		Fanout:            3,
		PingTimeout:       500 * time.Millisecond,
		IndirectPingCount: 3,
		RTTAlpha:          0.25,
	}
}

// rttTracker tracks round-trip times per peer using an exponentially
// weighted moving average (EWMA).
type rttTracker struct {
	mu    sync.Mutex
	rtts  map[int32]time.Duration
	alpha float64
}

func newRTTTracker(alpha float64) *rttTracker {
	return &rttTracker{
		rtts:  make(map[int32]time.Duration),
		alpha: alpha,
	}
}

// Update records a new RTT sample for the given peer and returns the updated EWMA.
func (r *rttTracker) Update(peerID int32, sample time.Duration) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	prev, ok := r.rtts[peerID]
	if !ok {
		r.rtts[peerID] = sample
		return sample
	}
	updated := time.Duration(float64(prev)*(1-r.alpha) + float64(sample)*r.alpha)
	r.rtts[peerID] = updated
	return updated
}

// Get returns the current EWMA RTT for the given peer, or 0 if unknown.
func (r *rttTracker) Get(peerID int32) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rtts[peerID]
}

// pendingPing tracks a ping awaiting an ack.
type pendingPing struct {
	pingID   uint64
	targetID int32
	sentAt   time.Time
	ackCh    chan struct{}
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

	rtt          *rttTracker
	pendingMu    sync.Mutex
	pendingPings map[uint64]*pendingPing
	nextPingID   uint64
}

// NewGossiper creates a new Gossiper.
func NewGossiper(cfg GossipConfig, transport Transport) *Gossiper {
	return &Gossiper{
		cfg:          cfg,
		transport:    transport,
		done:         make(chan struct{}),
		rtt:          newRTTTracker(cfg.RTTAlpha),
		pendingPings: make(map[uint64]*pendingPing),
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

// Start launches the heartbeat, ping, and suspicion loops.
func (g *Gossiper) Start() {
	g.wg.Add(3)
	go g.heartbeatLoop()
	go g.pingLoop()
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

// pingLoop periodically sends direct pings to a random peer and waits for acks.
// If a direct ping times out, it triggers an indirect ping through K other peers.
func (g *Gossiper) pingLoop() {
	defer g.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Gossip pingLoop panic recovered: %v (errno=%d)", r, syscall.EIO)
		}
	}()

	ticker := time.NewTicker(g.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.done:
			return
		case <-ticker.C:
			g.sendPing()
		}
	}
}

// sendPing sends a direct ping to one random alive peer and waits for an ack.
// On timeout, it initiates an indirect ping through K other peers.
func (g *Gossiper) sendPing() {
	peers := g.transport.Peers().RandomPeers(1, g.cfg.LocalID)
	if len(peers) == 0 {
		return
	}
	target := peers[0]
	pingID := atomic.AddUint64(&g.nextPingID, 1)
	now := time.Now().UnixNano()

	payload := &PingPayload{
		PingID:    pingID,
		TargetID:  target.ID,
		Timestamp: now,
	}
	rpc := &RPC{
		From:    g.cfg.LocalID,
		Type:    MsgPing,
		Payload: payload.Encode(),
	}

	pp := &pendingPing{
		pingID:   pingID,
		targetID: target.ID,
		sentAt:   time.Unix(0, now),
		ackCh:    make(chan struct{}, 1),
	}
	g.pendingMu.Lock()
	g.pendingPings[pingID] = pp
	g.pendingMu.Unlock()

	if err := g.transport.Send(target.ID, rpc); err != nil {
		log.Printf("Gossip ping to peer %d failed: %v (errno=%d)", target.ID, err, syscall.EHOSTUNREACH)
		g.removePendingPing(pingID)
		return
	}

	select {
	case <-pp.ackCh:
		rtt := time.Since(pp.sentAt)
		g.rtt.Update(target.ID, rtt)
		g.removePendingPing(pingID)
	case <-time.After(g.cfg.PingTimeout):
		g.removePendingPing(pingID)
		g.sendIndirectPing(target.ID, pingID, now)
	case <-g.done:
		g.removePendingPing(pingID)
		return
	}
}

// sendIndirectPing asks K random peers to ping the target on our behalf.
func (g *Gossiper) sendIndirectPing(targetID int32, pingID uint64, timestamp int64) {
	peers := g.transport.Peers().RandomPeers(g.cfg.IndirectPingCount, g.cfg.LocalID)
	if len(peers) == 0 {
		return
	}

	payload := &PingPayload{
		PingID:    pingID,
		TargetID:  targetID,
		Timestamp: timestamp,
	}
	rpc := &RPC{
		From:    g.cfg.LocalID,
		Type:    MsgIndirectPing,
		Payload: payload.Encode(),
	}

	indirectAck := make(chan struct{}, len(peers))
	var wg sync.WaitGroup
	for _, peer := range peers {
		if peer.ID == targetID {
			continue
		}
		wg.Add(1)
		go func(pid int32) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Gossip indirect ping goroutine panic recovered: %v (errno=%d)", r, syscall.EIO)
				}
			}()
			if err := g.transport.Send(pid, rpc); err != nil {
				log.Printf("Gossip indirect ping via peer %d failed: %v (errno=%d)", pid, err, syscall.EHOSTUNREACH)
				return
			}
			indirectAck <- struct{}{}
		}(peer.ID)
	}
	wg.Wait()

	if len(indirectAck) == 0 {
		if peer := g.transport.Peers().Get(targetID); peer != nil {
			if peer.State() == PeerStateAlive {
				peer.SetState(PeerStateSuspect)
				log.Printf("Gossip: peer %d marked SUSPECT (ping + indirect ping failed)", targetID)
			}
		}
	}
}

// removePendingPing removes a pending ping entry safely.
func (g *Gossiper) removePendingPing(pingID uint64) {
	g.pendingMu.Lock()
	delete(g.pendingPings, pingID)
	g.pendingMu.Unlock()
}

// handlePing responds to a direct ping with an ack.
func (g *Gossiper) handlePing(rpc *RPC) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Gossip handlePing panic recovered: %v (errno=%d)", r, syscall.EIO)
		}
	}()
	payload, err := DecodePingPayload(rpc.Payload)
	if err != nil {
		log.Printf("Gossip: failed to decode ping from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	ackPayload := &PingPayload{
		PingID:    payload.PingID,
		TargetID:  g.cfg.LocalID,
		Timestamp: payload.Timestamp,
	}
	ackRPC := &RPC{
		From:    g.cfg.LocalID,
		Type:    MsgAck,
		Payload: ackPayload.Encode(),
	}
	if err := g.transport.Send(rpc.From, ackRPC); err != nil {
		log.Printf("Gossip ack to peer %d failed: %v (errno=%d)", rpc.From, err, syscall.EHOSTUNREACH)
	}

	if peer := g.transport.Peers().Get(rpc.From); peer != nil {
		peer.Touch()
		if peer.State() == PeerStateSuspect {
			peer.SetState(PeerStateAlive)
			log.Printf("Gossip: peer %d restored to ALIVE via ping", rpc.From)
		}
	}
}

// handleAck matches an ack to a pending ping and signals the waiting goroutine.
func (g *Gossiper) handleAck(rpc *RPC) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Gossip handleAck panic recovered: %v (errno=%d)", r, syscall.EIO)
		}
	}()
	payload, err := DecodePingPayload(rpc.Payload)
	if err != nil {
		log.Printf("Gossip: failed to decode ack from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	g.pendingMu.Lock()
	pp, ok := g.pendingPings[payload.PingID]
	g.pendingMu.Unlock()
	if !ok {
		return
	}

	select {
	case pp.ackCh <- struct{}{}:
	default:
	}

	if peer := g.transport.Peers().Get(rpc.From); peer != nil {
		peer.Touch()
		if peer.State() == PeerStateSuspect {
			peer.SetState(PeerStateAlive)
			log.Printf("Gossip: peer %d restored to ALIVE via ack", rpc.From)
		}
	}
}

// handleIndirectPing forwards a ping to the target peer on behalf of the requester.
// If the target acks, the ack is forwarded back to the original requester.
func (g *Gossiper) handleIndirectPing(rpc *RPC) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Gossip handleIndirectPing panic recovered: %v (errno=%d)", r, syscall.EIO)
		}
	}()
	payload, err := DecodePingPayload(rpc.Payload)
	if err != nil {
		log.Printf("Gossip: failed to decode indirect ping from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	targetPeer := g.transport.Peers().Get(payload.TargetID)
	if targetPeer == nil {
		return
	}

	pingRPC := &RPC{
		From:    g.cfg.LocalID,
		Type:    MsgPing,
		Payload: payload.Encode(),
	}
	if err := g.transport.Send(payload.TargetID, pingRPC); err != nil {
		log.Printf("Gossip indirect ping to target %d failed: %v (errno=%d)", payload.TargetID, err, syscall.EHOSTUNREACH)
		return
	}

	indirectPP := &pendingPing{
		pingID:   payload.PingID,
		targetID: payload.TargetID,
		sentAt:   time.Unix(0, payload.Timestamp),
		ackCh:    make(chan struct{}, 1),
	}
	g.pendingMu.Lock()
	g.pendingPings[payload.PingID] = indirectPP
	g.pendingMu.Unlock()

	select {
	case <-indirectPP.ackCh:
		ackPayload := &PingPayload{
			PingID:    payload.PingID,
			TargetID:  payload.TargetID,
			Timestamp: payload.Timestamp,
		}
		ackRPC := &RPC{
			From:    g.cfg.LocalID,
			Type:    MsgAck,
			Payload: ackPayload.Encode(),
		}
		g.transport.Send(rpc.From, ackRPC)
		g.removePendingPing(payload.PingID)
	case <-time.After(g.cfg.PingTimeout):
		g.removePendingPing(payload.PingID)
	case <-g.done:
		g.removePendingPing(payload.PingID)
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
// Suspicion timeouts are adapted per-peer using RTT EWMA when available.
func (g *Gossiper) checkSuspicion() {
	now := time.Now()
	peers := g.transport.Peers().All()

	for _, p := range peers {
		if p.ID == g.cfg.LocalID {
			continue
		}
		elapsed := now.Sub(p.LastSeen())
		timeout := g.adaptiveTimeout(p.ID)
		switch p.State() {
		case PeerStateAlive:
			if elapsed > timeout {
				p.SetState(PeerStateSuspect)
				log.Printf("Gossip: peer %d marked SUSPECT (last seen %v ago, timeout=%v)", p.ID, elapsed, timeout)
			}
		case PeerStateSuspect:
			if elapsed > 2*timeout {
				p.SetState(PeerStateOffline)
				log.Printf("Gossip: peer %d marked OFFLINE (last seen %v ago, timeout=%v)", p.ID, elapsed, timeout)
				if g.onLeave != nil {
					g.onLeave(p.ID)
				}
			}
		}
	}
}

// adaptiveTimeout returns the suspicion timeout for a peer, adjusted by RTT.
// If no RTT data is available, falls back to the configured SuspicionTimeout.
func (g *Gossiper) adaptiveTimeout(peerID int32) time.Duration {
	rtt := g.rtt.Get(peerID)
	if rtt <= 0 {
		return g.cfg.SuspicionTimeout
	}
	adaptive := rtt * 10
	if adaptive < g.cfg.SuspicionTimeout {
		return g.cfg.SuspicionTimeout
	}
	if adaptive > 5*g.cfg.SuspicionTimeout {
		return 5 * g.cfg.SuspicionTimeout
	}
	return adaptive
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
	case MsgPing:
		g.handlePing(rpc)
	case MsgAck:
		g.handleAck(rpc)
	case MsgIndirectPing:
		g.handleIndirectPing(rpc)
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

	if peer := g.transport.Peers().Get(rpc.From); peer != nil {
		peer.Touch()
		if peer.State() == PeerStateSuspect {
			peer.SetState(PeerStateAlive)
			log.Printf("Gossip: peer %d restored to ALIVE via heartbeat", rpc.From)
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
