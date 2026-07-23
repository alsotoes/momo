package p2p

import (
	"fmt"
	"log"
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

	heldMu sync.Mutex
	held   map[uint64]*Lease

	pendingMu sync.Mutex
	pending   map[uint64]chan bool

	done chan struct{}
	wg   sync.WaitGroup
}

// NewLeaseManager creates a new LeaseManager.
func NewLeaseManager(localID int32, transport Transport) *LeaseManager {
	return &LeaseManager{
		localID:   localID,
		transport: transport,
		granted:   make(map[string]int64),
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
	close(lm.done)
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
	if ok && existing > now {
		lm.grantedMu.Unlock()
		grant := &LeasePayload{LeaseID: payload.LeaseID, Key: payload.Key, Expiry: 0}
		resp := &RPC{From: lm.localID, Type: MsgLeaseGrant, Payload: grant.Encode()}
		lm.transport.Send(rpc.From, resp)
		return
	}
	lm.granted[payload.Key] = payload.Expiry
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
	peers := lm.transport.Peers().Alive()
	peerCount := 0
	for _, p := range peers {
		if p.ID != lm.localID {
			peerCount++
		}
	}

	quorum := (peerCount+1)/2 + 1
	if quorum < 1 {
		quorum = 1
	}

	leaseID := lm.nextLeaseID.Add(1)
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
