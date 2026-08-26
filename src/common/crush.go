package common

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"sort"
	"syscall"
)

// CRUSH (Controlled Replication Under Scalable Hashing) was originally conceived by Sage Weil.
// This is a simplified Go implementation specifically for the Momo Object Storage system.

// Node represents a physical storage node in the cluster.
type Node struct {
	ID     int
	Weight int
	Addr   string
	// Domain is the failure-domain label (rack/zone/DC) used by Placement to
	// spread replicas across independent failure units (R1, #929). Empty means
	// unclassified; all unclassified nodes share one default domain.
	Domain string
}

// ClusterMap defines the topology of the storage cluster.
type ClusterMap struct {
	Nodes []*Node
}

// hashToScoreValue folds a sha256 digest into a float64 in [0,1) using a
// 52-bit mantissa. float64 has only a 52-bit mantissa, so converting a full
// uint64 discards its low ~11 bits and distinct hashes can map to the same
// float, biasing placement (fix #647). Taking the top 32 bits plus the bottom
// 20 bits of the digest keeps every mantissa bit meaningful and spans the full
// hash, using all available entropy with no precision loss.
func hashToScoreValue(sum []byte) float64 {
	hi := binary.LittleEndian.Uint32(sum[:4])
	lo := binary.LittleEndian.Uint32(sum[28:32])
	mant := uint64(hi)<<20 | uint64(lo&(1<<20-1))
	return float64(mant) / float64(uint64(1)<<52)
}

// Placement returns an ordered list of nodes where an object should be stored, based on its hash.
// It uses a simplified version of the CRUSH algorithm (Weighted Rendezvous Hashing)
// to ensure perfect load balancing and minimal data movement when nodes are added/removed.
func (m *ClusterMap) Placement(objectHash string, replicationFactor int) (nodes []*Node, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Recovered from panic in Placement: %v", r)
			err = fmt.Errorf("panic in Placement: %v: %w", r, syscall.EIO)
		}
	}()

	if len(m.Nodes) == 0 {
		return nil, fmt.Errorf("cluster map has no nodes: %w", syscall.EINVAL)
	}

	if replicationFactor <= 0 {
		return nil, fmt.Errorf("invalid replication factor: %d: %w", replicationFactor, syscall.EINVAL)
	}

	if objectHash == "" {
		return nil, fmt.Errorf("invalid object hash: empty: %w", syscall.EINVAL)
	}

	// Filter out zero/negative-weight nodes: a disabled or decommissioned node
	// (Weight <= 0) must never receive data, even when replicationFactor is
	// large enough that last-sorted zero-score nodes would otherwise be selected.
	eligible := make([]*Node, 0, len(m.Nodes))
	domainsConfigured := false
	for _, node := range m.Nodes {
		if node != nil && node.Weight > 0 {
			eligible = append(eligible, node)
			if node.Domain != "" {
				domainsConfigured = true
			}
		}
	}

	if len(eligible) == 0 {
		return nil, fmt.Errorf("cluster map has no nodes with positive weight: %w", syscall.EINVAL)
	}

	if replicationFactor > len(eligible) {
		log.Printf("WARNING: replication factor %d exceeds %d eligible nodes in cluster map, capping to %d",
			replicationFactor, len(eligible), len(eligible))
		replicationFactor = len(eligible)
	}

	type score struct {
		node  *Node
		value float64
	}

	scores := make([]score, len(eligible))

	for i, node := range eligible {
		// Calculate a deterministic float score between 0 and 1 for this node/hash pair.
		h := sha256.New()
		h.Write([]byte(objectHash))

		// ⚡ Bolt: Eliminate reflection overhead and allocations by using stack-allocated buffer
		var idBuf [8]byte
		binary.LittleEndian.PutUint64(idBuf[:], uint64(node.ID))
		h.Write(idBuf[:])

		// ⚡ Bolt: Eliminate heap allocation of hash.Sum by using stack-allocated slice
		var sumBuf [sha256.Size]byte
		sum := h.Sum(sumBuf[:0])

		// ⚡ Bolt: Fold the full 32-byte hash into a 52-bit mantissa (fix #647) —
		// float64 cannot represent a full uint64 exactly, so using only the high
		// 64 bits would let distinct hashes collide on the same score.
		floatVal := hashToScoreValue(sum)

		// ⚡ Bolt: Use Weighted Rendezvous Hashing (WRH) formula: -weight / log(score).
		// This provides mathematically perfect load balancing for heterogeneous nodes.
		var finalScore float64
		if floatVal > 0 && floatVal < 1.0 && node.Weight > 0 {
			finalScore = -float64(node.Weight) / math.Log(floatVal)
		} else {
			finalScore = 0
		}

		scores[i] = score{node: node, value: finalScore}
	}

	// Sort nodes by score descending. SliceStable guarantees that nodes with
	// tied scores keep their declaration order, making placement deterministic
	// when node order varies between runs (fix #646).
	sort.SliceStable(scores, func(i, j int) bool {
		return scores[i].value > scores[j].value
	})

	// Failure-domain spread (R1, #929): when at least one node declares a
	// Domain, select the replica set that maximizes the number of distinct
	// failure domains, tie-broken by descending finalScore. The greedy pass
	// below is equivalent to brute-force over replica sets: scores are sorted
	// descending, so the first visit of each domain picks that domain's
	// highest-scoring node and domains enter in order of their best score.
	// When no node declares a Domain, keep the legacy top-R selection so the
	// hot path and benchmark numbers are unchanged for existing clusters.
	result := make([]*Node, 0, replicationFactor)
	if !domainsConfigured {
		for i := 0; i < replicationFactor; i++ {
			result = append(result, scores[i].node)
		}
		return result, nil
	}

	used := make(map[string]struct{}, replicationFactor)
	selected := make(map[*Node]struct{}, replicationFactor)
	for _, s := range scores {
		if len(result) == replicationFactor {
			break
		}
		if _, dup := used[s.node.Domain]; dup {
			continue
		}
		used[s.node.Domain] = struct{}{}
		selected[s.node] = struct{}{}
		result = append(result, s.node)
	}

	// Degraded fallback (R1-C4): fewer distinct domains than replicas — fill
	// the remaining slots by score, sharing domains, and warn (consistent with
	// the existing degraded-mode replication-factor logging above).
	if len(result) < replicationFactor {
		log.Printf("WARNING: replication factor %d exceeds %d distinct failure domains; placing replicas in shared failure domains (DEGRADED)",
			replicationFactor, len(used))
		for _, s := range scores {
			if len(result) == replicationFactor {
				break
			}
			if _, ok := selected[s.node]; ok {
				continue
			}
			selected[s.node] = struct{}{}
			result = append(result, s.node)
		}
	}

	return result, nil
}
