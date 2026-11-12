package p2p

import (
	"fmt"
	"log"

	"github.com/alsotoes/momo/src/common"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Lease represents a time-bound, self-expiring lease granted by a majority quorum.
type Lease struct {
	LeaseID    uint64
	HolderID   int32
	Key        string
	Expiry     time.Time
	QuorumSize int
}

// LeaseManager handles lease-based consensus for destructive operations.
// Leases are kept in-memory and expire automatically on timeout.
type LeaseManager struct {
	localID   int32
	transport Transport

	nextLeaseID atomic.Uint64

	grantedMu sync.Mutex
	granted   map[string]int64
	// grantedID records which holder owns each granted key so the holder can
	// renew without releasing (which would open a mutual-exclusion gap).
	grantedID map[string]uint64

	heldMu sync.Mutex
	held   map[uint64]*Lease

	pendingMu sync.Mutex
	pending   map[uint64]chan bool

	stopMu sync.Mutex
	closed bool
	done   chan struct{}
	wg     sync.WaitGroup

	// clusterSize is the number of configured nodes, used for split-brain
	// prevention (quorum of the whole cluster, not of whichever nodes happen to
	// be reachable). Zero means "unknown": the manager falls back to the
	// reachable-peer heuristic so tests and single-process use keep working.
	clusterSize int

	// Coordination counters (coordinator failover Phase 5): proactive
	// resignations and quorum-loss suspensions, exposed as metrics.
	handoffs   atomic.Uint64
	quorumLoss atomic.Uint64
	// contentions counts lease requests denied because another holder still
	// owns the key — the signal that destructive operations are queueing.
	contentions atomic.Uint64
}

// CoordinationCounters returns (proactive handoffs, quorum-loss suspensions).
func (lm *LeaseManager) CoordinationCounters() (handoffs, quorumLoss uint64) {
	return lm.handoffs.Load(), lm.quorumLoss.Load()
}

// LeaseContentions returns how many lease requests this node denied because a
// different holder still owned the key. A rising value means destructive
// operations are contending; a value that only ever climbs means they are
// queued behind a stuck holder.
func (lm *LeaseManager) LeaseContentions() uint64 {
	return lm.contentions.Load()
}

// SetClusterSize declares the configured cluster size so lease acquisition
// requires a majority of the whole cluster and a partition cannot grant leases
// with a minority (split-brain prevention, coordinator failover Phase 3).
func (lm *LeaseManager) SetClusterSize(n int) {
	lm.stopMu.Lock()
	lm.clusterSize = n
	lm.stopMu.Unlock()
}

// ClusterQuorum returns the number of nodes (including self) that must be
// reachable for coordination, or 0 when the cluster size is unknown.
func (lm *LeaseManager) ClusterQuorum() int {
	lm.stopMu.Lock()
	size := lm.clusterSize
	lm.stopMu.Unlock()
	if size <= 0 {
		return 0
	}
	// Majority of the whole cluster, including self: 2 of 3, 3 of 5.
	return size/2 + 1
}

// HasQuorum reports whether enough nodes are reachable to coordinate.
func (lm *LeaseManager) HasQuorum() bool {
	need := lm.ClusterQuorum()
	if need == 0 {
		return true
	}
	reachable := 1 // self
	for _, p := range lm.transport.Peers().AliveByQuality() {
		if p.ID != lm.localID {
			reachable++
		}
	}
	return reachable >= need
}

// NewLeaseManager creates a new LeaseManager.
func NewLeaseManager(localID int32, transport Transport) *LeaseManager {
	return &LeaseManager{
		localID:   localID,
		transport: transport,
		granted:   make(map[string]int64),
		grantedID: make(map[string]uint64),
		held:      make(map[uint64]*Lease),
		pending:   make(map[uint64]chan bool),
		done:      make(chan struct{}),
	}
}

// Start launches the expiry loop that cleans up expired leases.
func (lm *LeaseManager) Start() {
	lm.wg.Add(1)
	go lm.expireLoop()
}

// Stop shuts down the lease manager.
func (lm *LeaseManager) Stop() {
	lm.stopMu.Lock()
	if lm.closed {
		lm.stopMu.Unlock()
		return
	}
	lm.closed = true
	close(lm.done)
	lm.stopMu.Unlock()
	lm.wg.Wait()
}

// expireLoop periodically cleans up expired locally-granted leases.
func (lm *LeaseManager) expireLoop() {
	defer lm.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("LeaseManager expireLoop panic recovered: %v (errno=%d)", r, syscall.EIO)
		}
	}()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-lm.done:
			return
		case <-ticker.C:
			lm.cleanupExpired()
		}
	}
}

// cleanupExpired removes expired leases from the granted and held maps.
func (lm *LeaseManager) cleanupExpired() {
	now := time.Now().UnixNano()

	lm.grantedMu.Lock()
	for key, expiry := range lm.granted {
		if expiry < now {
			delete(lm.granted, key)
		}
	}
	lm.grantedMu.Unlock()

	lm.heldMu.Lock()
	for id, lease := range lm.held {
		if lease.Expiry.Before(time.Now()) {
			delete(lm.held, id)
		}
	}
	lm.heldMu.Unlock()
}

// HandleLeaseRPC processes incoming lease RPCs from remote peers.
// Called by the Gossiper's consumer loop.
func (lm *LeaseManager) HandleLeaseRPC(rpc *RPC) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("LeaseManager HandleLeaseRPC panic recovered: %v (errno=%d)", r, syscall.EIO)
		}
	}()
	switch rpc.Type {
	case MsgLeaseRequest:
		lm.handleLeaseRequest(rpc)
	case MsgLeaseGrant:
		lm.handleLeaseGrant(rpc)
	case MsgLeaseRelease:
		lm.handleLeaseRelease(rpc)
	}
}

// handleLeaseRequest decides whether to grant a lease request from a remote peer.
func (lm *LeaseManager) handleLeaseRequest(rpc *RPC) {
	payload, err := DecodeLeasePayload(rpc.Payload)
	if err != nil {
		log.Printf("LeaseManager: failed to decode lease request from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	now := time.Now().UnixNano()

	lm.grantedMu.Lock()
	existing, ok := lm.granted[payload.Key]
	// A renewal from the current holder keeps the same lease ID; only a different
	// holder is denied. Releasing to re-acquire would open a mutual-exclusion gap.
	if ok && existing > now && lm.grantedID[payload.Key] != payload.LeaseID {
		lm.contentions.Add(1)
		lm.grantedMu.Unlock()
		grant := &LeasePayload{LeaseID: payload.LeaseID, Key: payload.Key, Expiry: 0}
		resp := &RPC{From: lm.localID, Type: MsgLeaseGrant, Payload: grant.Encode()}
		if err := lm.transport.Send(rpc.From, resp); err != nil {
			log.Printf("LeaseManager: failed to send denial to peer %d: %v (errno=%d)", rpc.From, err, syscall.EHOSTUNREACH)
		}
		return
	}
	lm.granted[payload.Key] = payload.Expiry
	lm.grantedID[payload.Key] = payload.LeaseID
	lm.grantedMu.Unlock()

	grant := &LeasePayload{LeaseID: payload.LeaseID, Key: payload.Key, Expiry: payload.Expiry}
	resp := &RPC{From: lm.localID, Type: MsgLeaseGrant, Payload: grant.Encode()}
	if err := lm.transport.Send(rpc.From, resp); err != nil {
		log.Printf("LeaseManager: failed to send grant to peer %d: %v (errno=%d)", rpc.From, err, syscall.EHOSTUNREACH)
	}
}

// handleLeaseGrant routes an incoming lease grant to the pending Acquire call.
func (lm *LeaseManager) handleLeaseGrant(rpc *RPC) {
	payload, err := DecodeLeasePayload(rpc.Payload)
	if err != nil {
		log.Printf("LeaseManager: failed to decode lease grant from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	lm.pendingMu.Lock()
	ch, ok := lm.pending[payload.LeaseID]
	lm.pendingMu.Unlock()

	if !ok {
		return
	}

	granted := payload.Expiry != 0
	select {
	case ch <- granted:
	default:
	}
}

// handleLeaseRelease releases a previously granted lease.
func (lm *LeaseManager) handleLeaseRelease(rpc *RPC) {
	payload, err := DecodeLeasePayload(rpc.Payload)
	if err != nil {
		log.Printf("LeaseManager: failed to decode lease release from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	lm.grantedMu.Lock()
	delete(lm.granted, payload.Key)
	lm.grantedMu.Unlock()
}

// Acquire requests a lease for the given resource key from a majority of alive peers.
// Returns the lease if the quorum is reached, or an error otherwise.
func (lm *LeaseManager) Acquire(key string, duration time.Duration) (*Lease, error) {
	return lm.request(key, lm.nextLeaseID.Add(1), duration)
}

// Renew extends a lease this node already holds without releasing it. Peers
// accept the request because the lease ID matches the current holder, so there
// is no window in which a competing node could acquire the key.
func (lm *LeaseManager) Renew(lease *Lease, duration time.Duration) (*Lease, error) {
	if lease == nil || lease.Key == "" || lease.LeaseID == 0 {
		return nil, fmt.Errorf("invalid lease to renew: %w", syscall.EINVAL)
	}
	return lm.request(lease.Key, lease.LeaseID, duration)
}

// request is the shared acquire/renew implementation.
func (lm *LeaseManager) request(key string, leaseID uint64, duration time.Duration) (*Lease, error) {
	// Quality-aware: prefer low-RTT alive peers for quorum (issue #823).
	peers := lm.transport.Peers().AliveByQuality()
	peerCount := 0
	for _, p := range peers {
		if p.ID != lm.localID {
			peerCount++
		}
	}

	if err := lm.checkCoordinationAllowed(peerCount); err != nil {
		return nil, err
	}

	quorum := majorityQuorum(peerCount)

	expiry := time.Now().Add(duration)

	payload := &LeasePayload{
		LeaseID: leaseID,
		Key:     key,
		Expiry:  expiry.UnixNano(),
	}

	rpc := &RPC{
		From:    lm.localID,
		Type:    MsgLeaseRequest,
		Payload: payload.Encode(),
	}

	grantCh := make(chan bool, peerCount)

	lm.pendingMu.Lock()
	lm.pending[leaseID] = grantCh
	lm.pendingMu.Unlock()

	defer func() {
		lm.pendingMu.Lock()
		delete(lm.pending, leaseID)
		lm.pendingMu.Unlock()
	}()

	sent := lm.transport.Broadcast(rpc)
	if sent < quorum {
		return nil, fmt.Errorf("not enough peers for quorum: contacted %d, need %d (errno=%d)", sent, quorum, syscall.EHOSTUNREACH)
	}

	grants := 0
	timer := time.NewTimer(duration / 2)
	defer timer.Stop()

	for grants < quorum {
		select {
		case granted := <-grantCh:
			if granted {
				grants++
			}
		case <-timer.C:
			return nil, fmt.Errorf("lease quorum timeout: got %d/%d grants (errno=%d)", grants, quorum, syscall.ETIMEDOUT)
		case <-lm.done:
			return nil, fmt.Errorf("lease manager stopped (errno=%d)", syscall.ECANCELED)
		}
	}

	lease := &Lease{
		LeaseID:    leaseID,
		HolderID:   lm.localID,
		Key:        key,
		Expiry:     expiry,
		QuorumSize: grants,
	}

	lm.heldMu.Lock()
	lm.held[leaseID] = lease
	lm.heldMu.Unlock()

	return lease, nil
}

// checkCoordinationAllowed enforces the split-brain guards before a lease
// request is broadcast: a known cluster size requires a majority of the whole
// cluster to be reachable, and configured-but-unreachable peers mean a
// partition rather than a single-node cluster (issues #629, #630).
func (lm *LeaseManager) checkCoordinationAllowed(peerCount int) error {
	if !lm.HasQuorum() {
		return fmt.Errorf("coordination suspended: cluster quorum lost: %w", syscall.EHOSTUNREACH)
	}
	if need := lm.ClusterQuorum(); need > 0 {
		if reachable := 1 + peerCount; reachable < need {
			return fmt.Errorf("coordination suspended: %d/%d nodes reachable, need %d (errno=%d)", reachable, lm.clusterSize, need, syscall.EHOSTUNREACH)
		}
	}
	if lm.transport.Peers().Count() > 0 && peerCount == 0 {
		return fmt.Errorf("no reachable peers for quorum: cluster partitioned (errno=%d)", syscall.EHOSTUNREACH)
	}
	return nil
}

// majorityQuorum returns the smallest integer quorum that requires a strict
// majority of peers: floor(n/2)+1 for n >= 1, and 0 for an empty cluster.
// Examples: 2 of 3, 3 of 5. The previous formula ((n+1)/2 + 1) used a
// supermajority for odd n (3 of 3, 4 of 5), so a single failure blocked every
// lease in a small cluster (issue #629).
func majorityQuorum(peerCount int) int {
	if peerCount == 0 {
		return 0
	}
	return peerCount/2 + 1
}

// Release broadcasts a lease release to all peers.
func (lm *LeaseManager) Release(lease *Lease) error {
	payload := &LeasePayload{
		LeaseID: lease.LeaseID,
		Key:     lease.Key,
		Expiry:  0,
	}

	rpc := &RPC{
		From:    lm.localID,
		Type:    MsgLeaseRelease,
		Payload: payload.Encode(),
	}

	lm.transport.Broadcast(rpc)

	lm.heldMu.Lock()
	delete(lm.held, lease.LeaseID)
	lm.heldMu.Unlock()

	return nil
}

// ReleaseByKey releases a held lease matching the given resource key.
func (lm *LeaseManager) ReleaseByKey(key string) error {
	lm.heldMu.Lock()
	for id, lease := range lm.held {
		if lease.Key == key {
			delete(lm.held, id)
			lm.heldMu.Unlock()

			payload := &LeasePayload{LeaseID: id, Key: key, Expiry: 0}
			rpc := &RPC{From: lm.localID, Type: MsgLeaseRelease, Payload: payload.Encode()}
			lm.transport.Broadcast(rpc)
			return nil
		}
	}
	lm.heldMu.Unlock()
	return nil
}

// OnPeerFailure reacts to a peer degrading to SUSPECT or OFFLINE: every held
// lease whose quorum is no longer satisfiable is released immediately, instead
// of waiting for the TTL to expire (coordinator failover, Phase 1).
//
// The rule is conservative on purpose: a lease records how many grants it
// needed, not which peers granted them, so if the number of reachable peers
// drops below that count the holder stops acting. Releasing broadcasts a
// release so a healthy node can take the role over right away.
func (lm *LeaseManager) OnPeerFailure(peerID int32) (err error) {
	defer common.RecoverErr("LeaseManager.OnPeerFailure", &err)
	alive := 0
	for _, p := range lm.transport.Peers().AliveByQuality() {
		if p.ID != lm.localID {
			alive++
		}
	}

	// Losing cluster quorum suspends all coordination: release everything, not
	// only the leases that are individually unsatisfiable.
	suspended := !lm.HasQuorum()

	var lost []*Lease
	lm.heldMu.Lock()
	for id, lease := range lm.held {
		if suspended || alive < lease.QuorumSize {
			lost = append(lost, lease)
			delete(lm.held, id)
		}
	}
	lm.heldMu.Unlock()

	if suspended {
		lm.quorumLoss.Add(1)
		log.Printf("AUDIT: coordination suspended: cluster quorum lost after peer %d failed", peerID)
	}

	for _, lease := range lost {
		log.Printf("AUDIT: coordination suspended for %q: peer %d failed, %d peer(s) reachable, lease required %d",
			lease.Key, peerID, alive, lease.QuorumSize)
		payload := &LeasePayload{LeaseID: lease.LeaseID, Key: lease.Key, Expiry: 0}
		lm.transport.Broadcast(&RPC{From: lm.localID, Type: MsgLeaseRelease, Payload: payload.Encode()})
	}
	return nil
}

// ResignAll releases every held lease and audits why. Used for proactive
// handoff when the node is unhealthy (coordinator failover Phase 2): stepping
// down early is cheaper than failing mid-operation.
func (lm *LeaseManager) ResignAll(reason string) (count int) {
	lm.heldMu.Lock()
	lost := make([]*Lease, 0, len(lm.held))
	for id, lease := range lm.held {
		lost = append(lost, lease)
		delete(lm.held, id)
	}
	lm.heldMu.Unlock()

	if len(lost) > 0 {
		lm.handoffs.Add(1)
	}
	for _, lease := range lost {
		log.Printf("AUDIT: resigning lease %q (%s)", lease.Key, reason)
		payload := &LeasePayload{LeaseID: lease.LeaseID, Key: lease.Key, Expiry: 0}
		lm.transport.Broadcast(&RPC{From: lm.localID, Type: MsgLeaseRelease, Payload: payload.Encode()})
	}
	return len(lost)
}

// ActiveLeases returns the number of leases currently held by this node.
// R5 scrape-time cluster gauge (momo_leases_active) (#933).
func (lm *LeaseManager) ActiveLeases() int {
	lm.heldMu.Lock()
	defer lm.heldMu.Unlock()
	return len(lm.held)
}
