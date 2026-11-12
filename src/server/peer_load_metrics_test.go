package server

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alsotoes/momo/src/p2p"
)

func TestPeerLoadsRenderedAsMetrics(t *testing.T) {
	m := NewMetricsCollector()
	m.SetClusterStats(&fakeClusterStats{loads: []PeerLoadSample{
		{PeerID: 2, DiskUsedPercent: 91, InflightRequests: 17},
		{PeerID: 3, DiskUsedPercent: 12, InflightRequests: 0},
	}})
	var buf bytes.Buffer
	m.writeMetrics(&buf)
	out := buf.String()
	for _, want := range []string{
		`momo_peer_disk_used_percent{peer="2"} 91`,
		`momo_peer_disk_used_percent{peer="3"} 12`,
		`momo_peer_inflight_requests{peer="2"} 17`,
		`momo_peer_inflight_requests{peer="3"} 0`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in metrics output", want)
		}
	}
}

func TestPeerLoadsAbsentWithoutReports(t *testing.T) {
	m := NewMetricsCollector()
	m.SetClusterStats(&fakeClusterStats{})
	var buf bytes.Buffer
	m.writeMetrics(&buf)
	out := buf.String()
	if strings.Contains(out, "momo_peer_disk_used_percent{") {
		t.Fatal("no samples expected without reports")
	}
	// The HELP/TYPE lines are always present so dashboards can chart absence.
	if !strings.Contains(out, "# TYPE momo_peer_disk_used_percent gauge") {
		t.Fatal("family declaration must always be emitted")
	}
}

func TestP2PClusterStatsPeerLoads(t *testing.T) {
	pm := p2p.NewPeerMap()
	alive := p2p.NewPeer(5, "127.0.0.1:1")
	alive.SetLoad(p2p.PeerLoad{DiskUsedPercent: 44, InflightRequests: 9})
	silent := p2p.NewPeer(6, "127.0.0.1:2")
	pm.Add(alive)
	pm.Add(silent)

	c := &p2pClusterStats{peers: pm}
	loads := c.PeerLoads()
	if len(loads) != 1 {
		t.Fatalf("only peers with a report should appear, got %d", len(loads))
	}
	if loads[0].PeerID != 5 || loads[0].DiskUsedPercent != 44 || loads[0].InflightRequests != 9 {
		t.Fatalf("unexpected sample: %+v", loads[0])
	}
}

func TestP2PClusterStatsPeerLoadsNilSafe(t *testing.T) {
	c := &p2pClusterStats{}
	if got := c.PeerLoads(); got != nil {
		t.Fatalf("nil peer map must yield no samples, got %v", got)
	}
}

func TestCoordinationCountersRendered(t *testing.T) {
	m := NewMetricsCollector()
	m.SetClusterStats(&fakeClusterStats{handoffs: 3, quorumLoss: 2})
	var buf bytes.Buffer
	m.writeMetrics(&buf)
	out := buf.String()
	for _, want := range []string{
		"momo_coordination_handoffs_total 3",
		"momo_coordination_quorum_loss_total 2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestP2PClusterStatsLeaseContentionsNilSafe(t *testing.T) {
	c := &p2pClusterStats{}
	if got := c.LeaseContentions(); got != 0 {
		t.Fatalf("nil lease manager must report zero contentions, got %d", got)
	}
}

func TestP2PClusterStatsLeaseContentionsReadsManager(t *testing.T) {
	lm := p2p.NewLeaseManager(1, nil)
	c := &p2pClusterStats{leases: lm}
	if got := c.LeaseContentions(); got != 0 {
		t.Fatalf("fresh lease manager must report zero contentions, got %d", got)
	}
}
