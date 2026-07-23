package p2p

import (
	"math/rand"
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

// RandomPeers returns up to k random alive peers, excluding the peer with excludeID.
func (m *PeerMap) RandomPeers(k int, excludeID int32) []*Peer {
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
