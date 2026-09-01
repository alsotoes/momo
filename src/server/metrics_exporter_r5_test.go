package server

import (
	"strings"
	"testing"
	"time"
)

// fakeStorageStats is a deterministic storageStatsProvider for exporter tests.
type fakeStorageStats struct {
	blobCount int64
	stored    int64
	dir       string
	gcRuns    uint64
	gcEvict   uint64
}

func (f *fakeStorageStats) Stats() (int64, int64, error) { return f.blobCount, f.stored, nil }
func (f *fakeStorageStats) DataDir() string              { return f.dir }
func (f *fakeStorageStats) GCMetrics() (uint64, uint64)  { return f.gcRuns, f.gcEvict }

// fakeClusterStats is a deterministic clusterStatsProvider for exporter tests.
type fakeClusterStats struct {
	peers    int
	alive    int
	suspect  int
	offline  int
	latency  float64
	leases   int
	queries  uint64
	timeouts uint64
}

func (f *fakeClusterStats) PeerCount() int { return f.peers }
func (f *fakeClusterStats) PeerStateCount(state int) int {
	return map[int]int{peerStateAlive: f.alive, peerStateSuspect: f.suspect, peerStateOffline: f.offline}[state]
}
func (f *fakeClusterStats) AvgPingLatencySeconds() float64    { return f.latency }
func (f *fakeClusterStats) ActiveLeases() int                 { return f.leases }
func (f *fakeClusterStats) ScatterCounters() (uint64, uint64) { return f.queries, f.timeouts }

// TestR5_ReplicationAndDedupCounters verifies replication bytes/failures and
// the dedup-hit counter surface in the scrape output.
func TestR5_ReplicationAndDedupCounters(t *testing.T) {
	mc := NewMetricsCollector()
	mc.IncDedupHits()
	mc.IncDedupHits()
	mc.IncDedupHits()
	mc.AddBytesReplicated(4096)
	mc.AddBytesReplicated(2048)
	mc.IncReplicationFailures()

	out := captureMetricsOutput(mc)
	for want, name := range map[string]string{
		"momo_dedup_hits_total 3":           "dedup hits",
		"momo_replication_bytes_total 6144": "replication bytes",
		"momo_replication_failures_total 1": "replication failures",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Expected %q (%s), got:\n%s", want, name, out)
		}
	}
}

// TestR5_StorageGaugesFromProvider verifies scrape-time storage/CAS gauges are
// emitted from the storageStatsProvider when installed.
func TestR5_StorageGaugesFromProvider(t *testing.T) {
	mc := NewMetricsCollector()
	mc.SetStorageStats(&fakeStorageStats{blobCount: 42, stored: 1048576, gcRuns: 7, gcEvict: 65536})

	out := captureMetricsOutput(mc)
	for want, name := range map[string]string{
		"momo_blob_count 42":              "blob count",
		"momo_stored_bytes_total 1048576": "stored bytes",
		"momo_cas_gc_runs_total 7":        "gc runs",
		"momo_cas_gc_evicted_bytes 65536": "gc evicted bytes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Expected %q (%s), got:\n%s", want, name, out)
		}
	}
	// Disk gauges must be present (Statfs on "." uses current fs; nonzero free).
	if !strings.Contains(out, "momo_disk_used_bytes ") || !strings.Contains(out, "momo_disk_free_bytes ") {
		t.Errorf("Expected disk gauges, got:\n%s", out)
	}
}

// TestR5_StorageGaugesZeroWithoutProvider verifies no storage provider yields
// explicit zero gauges (dashboards stay valid on non-CAS stores).
func TestR5_StorageGaugesZeroWithoutProvider(t *testing.T) {
	mc := NewMetricsCollector()
	out := captureMetricsOutput(mc)
	for want := range map[string]string{
		"momo_blob_count 0":         "",
		"momo_stored_bytes_total 0": "",
		"momo_disk_used_bytes 0":    "",
		"momo_disk_free_bytes 0":    "",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Expected %q in no-provider output, got:\n%s", want, out)
		}
	}
}

// TestR5_ClusterGauges verifies P2P/cluster gauges from the clusterStatsProvider.
func TestR5_ClusterGauges(t *testing.T) {
	mc := NewMetricsCollector()
	mc.SetClusterStats(&fakeClusterStats{
		peers: 5, alive: 3, suspect: 1, offline: 1, latency: 0.00125, leases: 2, queries: 100, timeouts: 4,
	})

	out := captureMetricsOutput(mc)
	for want, name := range map[string]string{
		"momo_cluster_peers 5":                     "cluster peers",
		"momo_swim_alive_count 3":                  "swim alive",
		"momo_swim_suspect_count 1":                "swim suspect",
		"momo_swim_offline_count 1":                "swim offline",
		"momo_swim_ping_latency_seconds 0.0012500": "swim latency",
		"momo_leases_active 2":                     "leases active",
		"momo_scatter_queries_total 100":           "scatter queries",
		"momo_scatter_timeout_total 4":             "scatter timeouts",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Expected %q (%s), got:\n%s", want, name, out)
		}
	}
}

// TestR5_ClusterGaugesZeroWithoutProvider verifies p2p-disabled (no provider)
// emits zero gauges, satisfying the spec's GIVEN p2p disabled contract.
func TestR5_ClusterGaugesZeroWithoutProvider(t *testing.T) {
	mc := NewMetricsCollector()
	out := captureMetricsOutput(mc)
	for want, name := range map[string]string{
		"momo_cluster_peers 0":         "cluster peers",
		"momo_swim_alive_count 0":      "swim alive",
		"momo_swim_suspect_count 0":    "swim suspect",
		"momo_leases_active 0":         "leases active",
		"momo_scatter_queries_total 0": "scatter queries",
		"momo_scatter_timeout_total 0": "scatter timeouts",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Expected %q (%s) with no provider, got:\n%s", want, name, out)
		}
	}
}

// TestR5_LatencyHistogramsOptIn verifies phase 4 histograms: disabled (default)
// emits empty _bucket/_count/_sum zero samples; enabled records observations.
func TestR5_LatencyHistogramsOptIn(t *testing.T) {
	mc := NewMetricsCollector()

	// Disabled: no operation buckets beyond documented zero +Inf.
	out := captureMetricsOutput(mc)
	if !strings.Contains(out, "momo_request_latency_seconds_count{operation=\"upload\"} 0") {
		t.Errorf("Expected disabled histogram zero-count line, got:\n%s", out)
	}
	if strings.Contains(out, "momo_request_latency_seconds_bucket{operation=\"upload\",le=\"0.005\"} 1") {
		t.Errorf("Disabled histograms must not accumulate any observations:\n%s", out)
	}

	// Enabled: observations flow into fixed buckets.
	mc.SetLatencyHistogramsEnabled(true)
	mc.RecordRequestLatency("upload", 2*time.Millisecond)
	mc.RecordRequestLatency("upload", 50*time.Millisecond)
	mc.RecordReplicationLatency(10 * time.Millisecond)

	out = captureMetricsOutput(mc)
	for want, name := range map[string]string{
		"momo_request_latency_seconds_bucket{operation=\"upload\",le=\"0.005\"} 1": "upload <=5ms",
		"momo_request_latency_seconds_bucket{operation=\"upload\",le=\"0.05\"} 2":  "upload 50ms folded into <=50ms",
		"momo_request_latency_seconds_count{operation=\"upload\"} 2":               "upload count",
		"momo_replication_latency_seconds_count 1":                                 "replication count",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Expected %q (%s), got:\n%s", want, name, out)
		}
	}
}

// TestR5_HistogramAtomic bucket cumulative invariant: each bucket count is
// non-decreasing towards +Inf and equals total count at +Inf.
func TestR5_HistogramAtomic(t *testing.T) {
	h := newAtomicLatencyHist()
	for i := 0; i < 100; i++ {
		h.observe(time.Duration(i) * time.Millisecond)
	}
	buckets, count, _ := h.samples()
	if count != 100 {
		t.Fatalf("expected total count 100, got %d", count)
	}
	if buckets[len(buckets)-1] != count {
		t.Fatalf("expected last bucket == total count (%d), got %d", count, buckets[len(buckets)-1])
	}
	for i := 1; i < len(buckets); i++ {
		if buckets[i] < buckets[i-1] {
			t.Fatalf("cumulative histogram must be non-decreasing at index %d: %d < %d", i, buckets[i], buckets[i-1])
		}
	}
}

// TestR5_LatencyEnabledNegative verifies HistogramsEnabled is false by default
// so the transport can skip time.Now() captures on the disabled path.
func TestR5_LatencyEnabledNegative(t *testing.T) {
	mc := NewMetricsCollector()
	if mc.HistogramsEnabled() {
		t.Fatal("expected histograms disabled by default")
	}
	if !mc.LatencyEnabled() == false {
		t.Fatal("expected LatencyEnabled false by default")
	}
	mc.SetLatencyHistogramsEnabled(true)
	if !mc.HistogramsEnabled() {
		t.Fatal("expected histograms enabled after opt-in")
	}
}
