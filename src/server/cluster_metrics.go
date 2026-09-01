package server

import (
	"github.com/alsotoes/momo/src/p2p"
)

// p2pClusterStats adapts the live P2P transport, lease, and scatter-gather
// state into the scrape-time cluster gauge source used by the metrics exporter
// (R5 phase 3, #933). It is nil-safe: any handle may be nil and the exporter
// degrades to zero gauge values (spec GIVEN p2p disabled).
type p2pClusterStats struct {
	peers        *p2p.PeerMap
	leases       *p2p.LeaseManager
	scatterGater *p2p.ScatterGather
}

// PeerCount implements clusterStatsProvider.
func (c *p2pClusterStats) PeerCount() int {
	if c.peers == nil {
		return 0
	}
	return c.peers.Count()
}

// PeerStateCount implements clusterStatsProvider. state uses the server-side
// peerState* constants which mirror the p2p PeerState values (alive=0,
// suspect=1, offline=2).
func (c *p2pClusterStats) PeerStateCount(state int) int {
	if c.peers == nil {
		return 0
	}
	return c.peers.StateCount(p2p.PeerState(state))
}

// AvgPingLatencySeconds implements clusterStatsProvider.
func (c *p2pClusterStats) AvgPingLatencySeconds() float64 {
	if c.peers == nil {
		return 0
	}
	return c.peers.AvgPingLatencySeconds()
}

// ActiveLeases implements clusterStatsProvider.
func (c *p2pClusterStats) ActiveLeases() int {
	if c.leases == nil {
		return 0
	}
	return c.leases.ActiveLeases()
}

// ScatterCounters implements clusterStatsProvider.
func (c *p2pClusterStats) ScatterCounters() (uint64, uint64) {
	if c.scatterGater == nil {
		return 0, 0
	}
	return c.scatterGater.ScatterCounters()
}

// newP2PClusterStats wires the available p2p handles, tolerating any nil
// (p2p disabled). The scatter handle also carries the peer map.
func newP2PClusterStats(scatterGather *p2p.ScatterGather, leaseManager *p2p.LeaseManager) clusterStatsProvider {
	if scatterGather == nil {
		return nil
	}
	return &p2pClusterStats{
		peers:        scatterGather.Peers(),
		leases:       leaseManager,
		scatterGater: scatterGather,
	}
}
