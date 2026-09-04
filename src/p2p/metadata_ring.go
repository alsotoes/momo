package p2p

import (
	"hash/crc32"
	"sort"
	"sync"
)

const (
	// MetadataShards is the number of shards in the consistent hash ring.
	// 256 shards provides good distribution for up to hundreds of nodes.
	MetadataShards = 256
	// MetadataVNodes is the number of virtual nodes per physical node.
	// 150 vnodes gives good load balancing even with small clusters.
	MetadataVNodes = 150
)

// Ring implements a consistent hash ring for metadata shard ownership.
// It uses CRC32 for fast, deterministic hashing of keys to shards.
type Ring struct {
	mu       sync.RWMutex
	shards   [MetadataShards]int32 // shard -> node ID
	vnodes   []vnodeEntry          // sorted by hash
	nodeIDs  map[int32]struct{}    // active node IDs
	replicaM int                   // metadata replication factor
}

type vnodeEntry struct {
	hash   uint32
	nodeID int32
}

var crc32Table = crc32.MakeTable(crc32.Castagnoli)

// NewRing creates a new consistent hash ring from the given node IDs.
func NewRing(nodeIDs []int32, replicaM int) *Ring {
	r := &Ring{
		nodeIDs:  make(map[int32]struct{}, len(nodeIDs)),
		replicaM: replicaM,
	}
	for _, id := range nodeIDs {
		r.nodeIDs[id] = struct{}{}
	}
	r.rebuild(nodeIDs)
	return r
}

// rebuild reconstructs the vnode list and shard assignments from nodeIDs.
func (r *Ring) rebuild(nodeIDs []int32) {
	r.vnodes = r.vnodes[:0]
	for _, id := range nodeIDs {
		for v := 0; v < MetadataVNodes; v++ {
			h := crc32.Checksum([]byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id), byte(v >> 8), byte(v)}, crc32Table)
			r.vnodes = append(r.vnodes, vnodeEntry{hash: h, nodeID: id})
		}
	}
	sort.Slice(r.vnodes, func(i, j int) bool {
		return r.vnodes[i].hash < r.vnodes[j].hash
	})
	// Assign shards to the nearest vnode clockwise
	for s := 0; s < MetadataShards; s++ {
		shardHash := uint32(s)
		idx := sort.Search(len(r.vnodes), func(i int) bool {
			return r.vnodes[i].hash >= shardHash
		})
		if idx == len(r.vnodes) {
			idx = 0
		}
		r.shards[s] = r.vnodes[idx].nodeID
	}
}

// UpdateNodes updates the ring with a new set of node IDs.
// Called when cluster membership changes (SWIM integration).
func (r *Ring) UpdateNodes(nodeIDs []int32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodeIDs = make(map[int32]struct{}, len(nodeIDs))
	for _, id := range nodeIDs {
		r.nodeIDs[id] = struct{}{}
	}
	r.rebuild(nodeIDs)
}

// Lookup returns the node ID owning the shard for the given key.
func (r *Ring) Lookup(key string) int32 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h := crc32.Checksum([]byte(key), crc32Table)
	s := int(h % MetadataShards)
	return r.shards[s]
}

// Replicas returns M distinct node IDs holding replicas for the given key.
// The first returned node is the primary shard owner.
func (r *Ring) Replicas(key string, M int) []int32 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	owner := r.Lookup(key)
	if M <= 1 {
		return []int32{owner}
	}
	var replicas []int32
	seen := make(map[int32]struct{}, M)
	replicas = append(replicas, owner)
	seen[owner] = struct{}{}
	h := crc32.Checksum([]byte(key), crc32Table)
	// Walk clockwise from the owner's vnode to find M-1 distinct successors
	vnodeHash := h
	for len(replicas) < M && len(replicas) < len(r.nodeIDs) {
		// Find next vnode after this hash
		idx := sort.Search(len(r.vnodes), func(i int) bool {
			return r.vnodes[i].hash > vnodeHash
		})
		if idx == len(r.vnodes) {
			idx = 0
		}
		candidate := r.vnodes[idx].nodeID
		if _, ok := seen[candidate]; !ok {
			replicas = append(replicas, candidate)
			seen[candidate] = struct{}{}
		}
		vnodeHash = r.vnodes[idx].hash
		if vnodeHash == r.vnodes[0].hash && idx == 0 {
			break // full circle
		}
	}
	return replicas
}

// ShardKey returns the shard key string for a given object name.
func ShardKey(name string) string {
	h := crc32.Checksum([]byte(name), crc32Table)
	return string(rune(h % MetadataShards))
}

// NodeCount returns the number of active nodes in the ring.
func (r *Ring) NodeCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.nodeIDs)
}
