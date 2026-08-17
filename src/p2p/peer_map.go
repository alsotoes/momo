package p2p

import (
	"math/rand"
	"sort"
	"sync"
	"time"
)

// PeerMap is a thread-safe map of peers keyed by peer ID.
type PeerMap struct {
	mu    sync.RWMutex
	peers map[int32]*Peer
	rng   *rand.Rand
}

// NewPeerMap creates a new empty PeerMap.
func NewPeerMap() *PeerMap {
	return &PeerMap{
		peers: make(map[int32]*Peer),
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Add adds or replaces a peer in the map.
func (m *PeerMap) Add(p *Peer) {
	m.mu.Lock()
	m.peers[p.ID] = p
	m.mu.Unlock()
}

// Get returns the peer with the given ID, or nil if not found.
func (m *PeerMap) Get(id int32) *Peer {
	m.mu.RLock()
	p := m.peers[id]
	m.mu.RUnlock()
	return p
}

// Remove removes a peer from the map.
func (m *PeerMap) Remove(id int32) {
	m.mu.Lock()
	delete(m.peers, id)
	m.mu.Unlock()
}

// All returns a snapshot of all peers. The returned slice is safe to iterate.
func (m *PeerMap) All() []*Peer {
	m.mu.RLock()
	result := make([]*Peer, 0, len(m.peers))
	for _, p := range m.peers {
		result = append(result, p)
	}
	m.mu.RUnlock()
	return result
}

// Alive returns all peers in PeerStateAlive state.
func (m *PeerMap) Alive() []*Peer {
	m.mu.RLock()
	result := make([]*Peer, 0, len(m.peers))
	for _, p := range m.peers {
		if p.State() == PeerStateAlive {
			result = append(result, p)
		}
	}
	m.mu.RUnlock()
	return result
}

// AliveByQuality returns all alive peers (excluding Suspect/Offline) sorted by
// EWMA RTT ascending, so the lowest-RTT (highest-quality) peers come first.
// Peers with an unknown RTT (0) sort after known-RTT peers but remain included
// while alive. This drives quality-aware quorum selection (issue #823).
func (m *PeerMap) AliveByQuality() []*Peer {
	m.mu.RLock()
	result := make([]*Peer, 0, len(m.peers))
	for _, p := range m.peers {
		if p.State() == PeerStateAlive {
			result = append(result, p)
		}
	}
	m.mu.RUnlock()

	sort.SliceStable(result, func(i, j int) bool {
		ri, rj := result[i].RTT(), result[j].RTT()
		// Unknown RTT (0) ranks last; otherwise ascending.
		if ri == 0 {
			if rj == 0 {
				return false
			}
			return false
		}
		if rj == 0 {
			return true
		}
		return ri < rj
	})
	return result
}

// AliveCount returns the number of peers in the PeerStateAlive state without
// allocating a slice (used by adaptive gossip fanout, issue #825).
func (m *PeerMap) AliveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, p := range m.peers {
		if p.State() == PeerStateAlive {
			n++
		}
	}
	return n
}

// RandomPeers returns up to k random alive peers, excluding the peer with excludeID.
func (m *PeerMap) RandomPeers(k int, excludeID int32) []*Peer {
	if k <= 0 {
		return nil
	}
	alive := m.Alive()
	if len(alive) <= 1 {
		result := make([]*Peer, 0)
		for _, p := range alive {
			if p.ID != excludeID {
				result = append(result, p)
			}
		}
		return result
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.rng.Shuffle(len(alive), func(i, j int) {
		alive[i], alive[j] = alive[j], alive[i]
	})

	result := make([]*Peer, 0, k)
	for _, p := range alive {
		if p.ID == excludeID {
			continue
		}
		result = append(result, p)
		if len(result) >= k {
			break
		}
	}
	return result
}

// Count returns the number of peers in the map.
func (m *PeerMap) Count() int {
	m.mu.RLock()
	n := len(m.peers)
	m.mu.RUnlock()
	return n
}

// PeerInfos returns a snapshot of all peers as PeerInfo structs (for gossip payloads).
func (m *PeerMap) PeerInfos() []PeerInfo {
	m.mu.RLock()
	result := make([]PeerInfo, 0, len(m.peers))
	for _, p := range m.peers {
		result = append(result, PeerInfo{ID: p.ID, Addr: p.Addr})
	}
	m.mu.RUnlock()
	return result
}
