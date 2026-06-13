package common

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sort"
)

// CRUSH (Controlled Replication Under Scalable Hashing) was originally conceived by Sage Weil.
// This is a simplified Go implementation specifically for the Momo Object Storage system.

// Node represents a physical storage node in the cluster.
type Node struct {
	ID     int
	Weight int
	Addr   string
}

// ClusterMap defines the topology of the storage cluster.
type ClusterMap struct {
	Nodes []*Node
}

// Placement returns an ordered list of nodes where an object should be stored, based on its hash.
// It uses a simplified version of the CRUSH algorithm (Weighted Rendezvous Hashing)
// to ensure perfect load balancing and minimal data movement when nodes are added/removed.
func (m *ClusterMap) Placement(objectHash string, replicationFactor int) ([]*Node, error) {
	if len(m.Nodes) == 0 {
		return nil, fmt.Errorf("cluster map has no nodes")
	}

	if replicationFactor > len(m.Nodes) {
		replicationFactor = len(m.Nodes)
	}

	type score struct {
		node  *Node
		value float64
	}

	scores := make([]score, len(m.Nodes))

	for i, node := range m.Nodes {
		// Calculate a deterministic float score between 0 and 1 for this node/hash pair.
		h := sha256.New()
		io.WriteString(h, objectHash)
		var idBuf [4]byte
		binary.LittleEndian.PutUint32(idBuf[:], uint32(node.ID))
		h.Write(idBuf[:])
		var sumBuf [sha256.Size]byte
		sum := h.Sum(sumBuf[:0])
		
		val := binary.LittleEndian.Uint64(sum[:8])
		floatVal := float64(val) / float64(math.MaxUint64)

		// ⚡ Bolt: Use Weighted Rendezvous Hashing (WRH) formula: -weight / log(score).
		// This provides mathematically perfect load balancing for heterogeneous nodes.
		var finalScore float64
		if floatVal > 0 && node.Weight > 0 {
			finalScore = -float64(node.Weight) / math.Log(floatVal)
		} else {
			finalScore = 0
		}

		scores[i] = score{node: node, value: finalScore}
	}

	// Sort nodes by score descending.
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].value > scores[j].value
	})

	result := make([]*Node, replicationFactor)
	for i := 0; i < replicationFactor; i++ {
		result[i] = scores[i].node
	}

	return result, nil
}
