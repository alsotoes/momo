package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/metrics"
)

// securityHeadersMiddleware adds standard security headers to HTTP responses.
// 🛡️ Sentinel: Medium priority enhancement to prevent XSS, clickjacking, and MIME sniffing.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		next.ServeHTTP(w, r)
	})
}

// storageStatsProvider is the optional scrape-time storage/CAS gauge source
// implemented by *storage.CASStore (R5 phase 2, #933).
type storageStatsProvider interface {
	Stats() (blobCount int64, storedBytes int64, err error)
	DataDir() string
	GCMetrics() (runs uint64, evicted uint64)
	GetDiskUsage() (used, free uint64, err error)
	ScrubStatus() (running bool, lastRun int64, errors uint64)
}

// clusterStatsProvider is the optional scrape-time P2P/cluster gauge source
// (R5 phase 3, #933). Implemented by the p2p transport/lease/scatter wiring.
type clusterStatsProvider interface {
	PeerCount() int
	PeerStateCount(state int) int
	AvgPingLatencySeconds() float64
	ActiveLeases() int
	ScatterCounters() (queries, timeouts uint64)
	GetReplicationStatus() (mode string, pending int)
	PeerLoads() []PeerLoadSample
	CoordinationCounters() (handoffs, quorumLoss uint64)
}

// PeerLoadSample is one peer's last published load report.
type PeerLoadSample struct {
	PeerID           int32
	DiskUsedPercent  uint8
	InflightRequests uint32
}

// swinState constants mirrored from the p2p package so the exporter does not
// import the p2p types just to distinguish gauge labels.
const (
	peerStateAlive   = 0
	peerStateSuspect = 1
	peerStateOffline = 2
)

// MetricsCollector tracks server-level metrics for Prometheus export.
type MetricsCollector struct {
	connectionsTotal  atomic.Uint64
	uploadsTotal      atomic.Uint64
	downloadsTotal    atomic.Uint64
	deletesTotal      atomic.Uint64
	replicationTotal  atomic.Uint64
	errorsTotal       atomic.Uint64
	activeConnections atomic.Int64
	bytesUploaded     atomic.Uint64
	bytesDownloaded   atomic.Uint64
	rebalanceMoved    atomic.Uint64
	rebalanceBytes    atomic.Uint64
	rebalanceCycles   atomic.Uint64
	rebalanceFailures atomic.Uint64
	startTime         time.Time
	hostname          string

	// R5 phase 2: dedup + GC counters (incremented in server + storage paths).
	dedupHitsTotal   atomic.Uint64
	gcRunsTotal      atomic.Uint64
	gcEvictedBytes   atomic.Uint64
	replicationBytes atomic.Uint64
	replicationFails atomic.Uint64

	// R5 phase 2/3: scrape-time providers (optional; nil-ped gracefully).
	storageOverride storageStatsProvider
	clusterOverride clusterStatsProvider

	// R5 phase 4: opt-in fixed-bucket latency histograms (atomic only).
	histogramsEnabled atomic.Bool
	requestLatency    atomicLatencyHist
	replicationLat    atomicLatencyHist

	// learnedState optionally supplies the runtime-learned inventory rendered
	// by /health/detailed (#1287). Nil omits the block (backward compatible).
	learnedState func() *LearnedState
}

// SetLearnedState installs the learned-state provider for /health/detailed.
func (m *MetricsCollector) SetLearnedState(fn func() *LearnedState) {
	m.learnedState = fn
}

// histogramBucketsSeconds are the fixed <= bucket upper bounds for the
// request/replication latency histograms (R5 phase 4, #933).
var histogramBucketsSeconds = []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10}

// atomicLatencyHist is a fixed-bucket Prometheus histogram backed only by
// sync/atomic counters (spec §no-external-deps). Buckets are cumulative
// le-bounds; counts and sumSeconds are atomic.
type atomicLatencyHist struct {
	counts     []atomic.Uint64
	totalCount atomic.Uint64
	totalSumNs atomic.Uint64
}

func newAtomicLatencyHist() atomicLatencyHist {
	return atomicLatencyHist{counts: make([]atomic.Uint64, len(histogramBucketsSeconds))}
}

func (h *atomicLatencyHist) observe(d time.Duration) {
	ns := uint64(d)
	if ns < 0 {
		ns = 0
	}
	h.totalCount.Add(1)
	h.totalSumNs.Add(ns)
	sec := d.Seconds()
	for i := range histogramBucketsSeconds {
		if sec <= histogramBucketsSeconds[i] {
			h.counts[i].Add(1)
		}
	}
}

// histSamples returns the cumulative bucket counts, total count, and total sum
// in seconds for the histogram.
func (h *atomicLatencyHist) samples() ([]uint64, uint64, float64) {
	out := make([]uint64, len(h.counts))
	for i := range h.counts {
		out[i] = h.counts[i].Load()
	}
	return out, h.totalCount.Load(), float64(h.totalSumNs.Load()) / 1e9
}

// NewMetricsCollector creates a new MetricsCollector.
// The process hostname is resolved once at construction and cached, so it is
// not re-fetched via a syscall on every /metrics scrape.
func NewMetricsCollector() *MetricsCollector {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
		log.Printf("AUDIT: os.Hostname() failed: %v", err)
	}
	return &MetricsCollector{
		startTime:      time.Now(),
		hostname:       hostname,
		requestLatency: newAtomicLatencyHist(),
		replicationLat: newAtomicLatencyHist(),
	}
}

// IncConnections records a new accepted connection (total and active).
func (m *MetricsCollector) IncConnections() { m.connectionsTotal.Add(1); m.activeConnections.Add(1) }

// DecConnections records a closed connection (active count).
func (m *MetricsCollector) DecConnections() { m.activeConnections.Add(-1) }

// IncUploads records a completed upload.
func (m *MetricsCollector) IncUploads() { m.uploadsTotal.Add(1) }

// IncDownloads records a completed download.
func (m *MetricsCollector) IncDownloads() { m.downloadsTotal.Add(1) }

// IncDeletes records a completed delete.
func (m *MetricsCollector) IncDeletes() { m.deletesTotal.Add(1) }

// IncReplication records a replication forward.
func (m *MetricsCollector) IncReplication() { m.replicationTotal.Add(1) }

// IncErrors records an operation error.
func (m *MetricsCollector) IncErrors() { m.errorsTotal.Add(1) }

// AddBytesUploaded adds n bytes to the uploaded-byte counter.
func (m *MetricsCollector) AddBytesUploaded(n uint64) { m.bytesUploaded.Add(n) }

// AddBytesDownloaded adds n bytes to the downloaded-byte counter.
func (m *MetricsCollector) AddBytesDownloaded(n uint64) { m.bytesDownloaded.Add(n) }

// IncDedupHits records a content-addressable dedup hit (R5 phase 2).
func (m *MetricsCollector) IncDedupHits() { m.dedupHitsTotal.Add(1) }

// AddBytesReplicated records bytes forwarded during replication (R5 phase 3).
func (m *MetricsCollector) AddBytesReplicated(n uint64) {
	m.replicationTotal.Add(1)
	m.replicationBytes.Add(n)
}

// IncReplicationFailures records a replication forwarding failure (R5 phase 3).
func (m *MetricsCollector) IncReplicationFailures() { m.replicationFails.Add(1) }

// ActiveConnections reports the current number of open connections (used by
// the gossip load publisher, R11 step 1).
func (m *MetricsCollector) ActiveConnections() uint32 {
	n := m.activeConnections.Load()
	if n < 0 {
		return 0
	}
	return uint32(n)
}

// IncRebalanceMoved counts blobs relocated by the R11 rebalance path.
func (m *MetricsCollector) IncRebalanceMoved() { m.rebalanceMoved.Add(1) }

// AddRebalanceBytes counts bytes relocated by the R11 rebalance path.
func (m *MetricsCollector) AddRebalanceBytes(n uint64) { m.rebalanceBytes.Add(n) }

// IncRebalanceCycles counts rebalance evaluations.
func (m *MetricsCollector) IncRebalanceCycles() { m.rebalanceCycles.Add(1) }

// IncRebalanceFailures counts rebalance pushes that failed or were aborted.
func (m *MetricsCollector) IncRebalanceFailures() { m.rebalanceFailures.Add(1) }

// SetStorageStats installs the optional scrape-time storage/CAS gauge source
// (R5 phase 2). A nil source is allowed and skips those gauges.
func (m *MetricsCollector) SetStorageStats(s storageStatsProvider) { m.storageOverride = s }

// SetClusterStats installs the optional scrape-time P2P/cluster gauge source
// (R5 phase 3). A nil source is allowed and skips those gauges.
func (m *MetricsCollector) SetClusterStats(c clusterStatsProvider) { m.clusterOverride = c }

// SetLatencyHistogramsEnabled toggles R5 phase 4 histograms. When false
// (default) there is zero overhead on the request path.
func (m *MetricsCollector) SetLatencyHistogramsEnabled(enabled bool) {
	m.histogramsEnabled.Store(enabled)
}

// HistogramsEnabled reports whether latency histograms are armed (R5 phase 4).
// Callers must check before capturing time.Now() to keep disabled path free of
// timing cost.
func (m *MetricsCollector) HistogramsEnabled() bool { return m.histogramsEnabled.Load() }

// LatencyEnabled implements transport.LatencyRecorder (R5 phase 4).
func (m *MetricsCollector) LatencyEnabled() bool { return m.histogramsEnabled.Load() }

// RecordRequestLatency observes an operation's duration into
// momo_request_latency_seconds. A no-op when disabled; the caller already
// guarded with HistogramsEnabled().
func (m *MetricsCollector) RecordRequestLatency(op string, d time.Duration) {
	m.requestLatency.observe(d)
}

// RecordOperationLatency is an alias for transport-facing recording.
func (m *MetricsCollector) RecordOperationLatency(op string, d time.Duration) {
	m.requestLatency.observe(d)
}

// RecordReplicationLatency observes a replication transfer's duration into
// momo_replication_latency_seconds.
func (m *MetricsCollector) RecordReplicationLatency(d time.Duration) {
	m.replicationLat.observe(d)
}

func (m *MetricsCollector) writeMetrics(w io.Writer) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	uptime := time.Since(m.startTime).Seconds()

	fmt.Fprintf(w, "# HELP momo_connections_total Total number of connections accepted.\n")
	fmt.Fprintf(w, "# TYPE momo_connections_total counter\n")
	fmt.Fprintf(w, "momo_connections_total %d\n", m.connectionsTotal.Load())

	fmt.Fprintf(w, "# HELP momo_active_connections Current number of active connections.\n")
	fmt.Fprintf(w, "# TYPE momo_active_connections gauge\n")
	fmt.Fprintf(w, "momo_active_connections %d\n", m.activeConnections.Load())

	fmt.Fprintf(w, "# HELP momo_uploads_total Total number of file uploads.\n")
	fmt.Fprintf(w, "# TYPE momo_uploads_total counter\n")
	fmt.Fprintf(w, "momo_uploads_total %d\n", m.uploadsTotal.Load())

	fmt.Fprintf(w, "# HELP momo_downloads_total Total number of file downloads.\n")
	fmt.Fprintf(w, "# TYPE momo_downloads_total counter\n")
	fmt.Fprintf(w, "momo_downloads_total %d\n", m.downloadsTotal.Load())

	fmt.Fprintf(w, "# HELP momo_deletes_total Total number of file deletes.\n")
	fmt.Fprintf(w, "# TYPE momo_deletes_total counter\n")
	fmt.Fprintf(w, "momo_deletes_total %d\n", m.deletesTotal.Load())

	_, _ = fmt.Fprintf(w, "# HELP momo_rebalance_blobs_moved_total Blobs relocated by the rebalance loop.\n")
	_, _ = fmt.Fprintf(w, "# TYPE momo_rebalance_blobs_moved_total counter\n")
	_, _ = fmt.Fprintf(w, "momo_rebalance_blobs_moved_total %d\n", m.rebalanceMoved.Load())

	_, _ = fmt.Fprintf(w, "# HELP momo_rebalance_bytes_moved_total Bytes relocated by the rebalance loop.\n")
	_, _ = fmt.Fprintf(w, "# TYPE momo_rebalance_bytes_moved_total counter\n")
	_, _ = fmt.Fprintf(w, "momo_rebalance_bytes_moved_total %d\n", m.rebalanceBytes.Load())

	_, _ = fmt.Fprintf(w, "# HELP momo_rebalance_cycles_total Rebalance evaluations run.\n")
	_, _ = fmt.Fprintf(w, "# TYPE momo_rebalance_cycles_total counter\n")
	_, _ = fmt.Fprintf(w, "momo_rebalance_cycles_total %d\n", m.rebalanceCycles.Load())

	_, _ = fmt.Fprintf(w, "# HELP momo_rebalance_failures_total Rebalance pushes that failed or were aborted.\n")
	_, _ = fmt.Fprintf(w, "# TYPE momo_rebalance_failures_total counter\n")
	_, _ = fmt.Fprintf(w, "momo_rebalance_failures_total %d\n", m.rebalanceFailures.Load())

	fmt.Fprintf(w, "# HELP momo_replication_total Total number of replication operations.\n")
	fmt.Fprintf(w, "# TYPE momo_replication_total counter\n")
	fmt.Fprintf(w, "momo_replication_total %d\n", m.replicationTotal.Load())

	fmt.Fprintf(w, "# HELP momo_errors_total Total number of errors.\n")
	fmt.Fprintf(w, "# TYPE momo_errors_total counter\n")
	fmt.Fprintf(w, "momo_errors_total %d\n", m.errorsTotal.Load())

	fmt.Fprintf(w, "# HELP momo_bytes_uploaded_total Total bytes uploaded.\n")
	fmt.Fprintf(w, "# TYPE momo_bytes_uploaded_total counter\n")
	fmt.Fprintf(w, "momo_bytes_uploaded_total %d\n", m.bytesUploaded.Load())

	fmt.Fprintf(w, "# HELP momo_bytes_downloaded_total Total bytes downloaded.\n")
	fmt.Fprintf(w, "# TYPE momo_bytes_downloaded_total counter\n")
	fmt.Fprintf(w, "momo_bytes_downloaded_total %d\n", m.bytesDownloaded.Load())

	fmt.Fprintf(w, "# HELP momo_uptime_seconds Server uptime in seconds.\n")
	fmt.Fprintf(w, "# TYPE momo_uptime_seconds gauge\n")
	fmt.Fprintf(w, "momo_uptime_seconds %.2f\n", uptime)

	fmt.Fprintf(w, "# HELP momo_goroutines Current number of goroutines.\n")
	fmt.Fprintf(w, "# TYPE momo_goroutines gauge\n")
	fmt.Fprintf(w, "momo_goroutines %d\n", runtime.NumGoroutine())

	fmt.Fprintf(w, "# HELP momo_memory_alloc_bytes Allocated memory in bytes.\n")
	fmt.Fprintf(w, "# TYPE momo_memory_alloc_bytes gauge\n")
	fmt.Fprintf(w, "momo_memory_alloc_bytes %d\n", memStats.Alloc)

	fmt.Fprintf(w, "# HELP momo_memory_sys_bytes System memory in bytes.\n")
	fmt.Fprintf(w, "# TYPE momo_memory_sys_bytes gauge\n")
	fmt.Fprintf(w, "momo_memory_sys_bytes %d\n", memStats.Sys)

	fmt.Fprintf(w, "# HELP momo_gc_runs_total Total number of GC runs.\n")
	fmt.Fprintf(w, "# TYPE momo_gc_runs_total counter\n")
	fmt.Fprintf(w, "momo_gc_runs_total %d\n", memStats.NumGC)

	// R5 phase 2: replication + dedup/GC counters.
	fmt.Fprintf(w, "# HELP momo_replication_bytes_total Total bytes replicated.\n")
	fmt.Fprintf(w, "# TYPE momo_replication_bytes_total counter\n")
	fmt.Fprintf(w, "momo_replication_bytes_total %d\n", m.replicationBytes.Load())

	fmt.Fprintf(w, "# HELP momo_replication_failures_total Total replication forwarding failures.\n")
	fmt.Fprintf(w, "# TYPE momo_replication_failures_total counter\n")
	fmt.Fprintf(w, "momo_replication_failures_total %d\n", m.replicationFails.Load())

	fmt.Fprintf(w, "# HELP momo_dedup_hits_total Total content-addressable dedup hits.\n")
	fmt.Fprintf(w, "# TYPE momo_dedup_hits_total counter\n")
	fmt.Fprintf(w, "momo_dedup_hits_total %d\n", m.dedupHitsTotal.Load())

	m.writeStorageMetrics(w)
	m.writeClusterMetrics(w)
	m.writeLatencyHistograms(w)

	fmt.Fprintf(w, "# HELP momo_build_info Build information.\n")
	fmt.Fprintf(w, "# TYPE momo_build_info gauge\n")
	fmt.Fprintf(w, "momo_build_info{hostname=\"%s\"} 1\n", m.hostname)
}

// writeStorageMetrics emits R5 phase 2 storage/CAS gauges from the optional
// storageStatsProvider (scrape-time only, never hot-path).
func (m *MetricsCollector) writeStorageMetrics(w io.Writer) {
	ss := m.storageOverride
	// If no override, emit zero-value gauges so dashboards stay valid.
	if ss == nil {
		fmt.Fprintf(w, "# HELP momo_blob_count Number of unique blobs in the CAS store.\n")
		fmt.Fprintf(w, "# TYPE momo_blob_count gauge\n")
		fmt.Fprintf(w, "momo_blob_count 0\n")
		fmt.Fprintf(w, "# HELP momo_stored_bytes_total Total logical bytes stored.\n")
		fmt.Fprintf(w, "# TYPE momo_stored_bytes_total gauge\n")
		fmt.Fprintf(w, "momo_stored_bytes_total 0\n")
		fmt.Fprintf(w, "# HELP momo_cas_gc_runs_total Number of CAS GC sweeps.\n")
		fmt.Fprintf(w, "# TYPE momo_cas_gc_runs_total counter\n")
		fmt.Fprintf(w, "momo_cas_gc_runs_total %d\n", m.gcRunsTotal.Load())
		fmt.Fprintf(w, "# HELP momo_cas_gc_evicted_bytes Total bytes evicted by CAS GC.\n")
		fmt.Fprintf(w, "# TYPE momo_cas_gc_evicted_bytes counter\n")
		fmt.Fprintf(w, "momo_cas_gc_evicted_bytes %d\n", m.gcEvictedBytes.Load())
		fmt.Fprintf(w, "# HELP momo_disk_used_bytes Disk bytes used in the data dir.\n")
		fmt.Fprintf(w, "# TYPE momo_disk_used_bytes gauge\n")
		fmt.Fprintf(w, "momo_disk_used_bytes 0\n")
		fmt.Fprintf(w, "# HELP momo_disk_free_bytes Disk bytes free in the data dir.\n")
		fmt.Fprintf(w, "# TYPE momo_disk_free_bytes gauge\n")
		fmt.Fprintf(w, "momo_disk_free_bytes 0\n")
		return
	}

	blobCount, storedBytes, err := ss.Stats()
	if err != nil {
		log.Printf("metrics: storage Stats() failed: %v", err)
		blobCount, storedBytes = 0, 0
	}
	gcRuns, gcEvicted := ss.GCMetrics()
	m.gcRunsTotal.Store(gcRuns)
	m.gcEvictedBytes.Store(gcEvicted)

	fmt.Fprintf(w, "# HELP momo_blob_count Number of unique blobs in the CAS store.\n")
	fmt.Fprintf(w, "# TYPE momo_blob_count gauge\n")
	fmt.Fprintf(w, "momo_blob_count %d\n", blobCount)
	fmt.Fprintf(w, "# HELP momo_stored_bytes_total Total logical bytes stored.\n")
	fmt.Fprintf(w, "# TYPE momo_stored_bytes_total gauge\n")
	fmt.Fprintf(w, "momo_stored_bytes_total %d\n", storedBytes)
	fmt.Fprintf(w, "# HELP momo_cas_gc_runs_total Number of CAS GC sweeps.\n")
	fmt.Fprintf(w, "# TYPE momo_cas_gc_runs_total counter\n")
	fmt.Fprintf(w, "momo_cas_gc_runs_total %d\n", gcRuns)
	fmt.Fprintf(w, "# HELP momo_cas_gc_evicted_bytes Total bytes evicted by CAS GC.\n")
	fmt.Fprintf(w, "# TYPE momo_cas_gc_evicted_bytes counter\n")
	fmt.Fprintf(w, "momo_cas_gc_evicted_bytes %d\n", gcEvicted)

	var stat syscall.Statfs_t
	if err := syscall.Statfs(ss.DataDir(), &stat); err == nil {
		// blocks (Bavail/Bfree) * bsize = bytes; use Bavail for the "free" gauge.
		bsize := uint64(stat.Bsize)
		if bsize == 0 {
			bsize = 512
		}
		free := stat.Bavail * bsize
		total := stat.Blocks * bsize
		used := uint64(0)
		if total > free {
			used = total - free
		}
		fmt.Fprintf(w, "# HELP momo_disk_used_bytes Disk bytes used in the data dir.\n")
		fmt.Fprintf(w, "# TYPE momo_disk_used_bytes gauge\n")
		fmt.Fprintf(w, "momo_disk_used_bytes %d\n", used)
		fmt.Fprintf(w, "# HELP momo_disk_free_bytes Disk bytes free in the data dir.\n")
		fmt.Fprintf(w, "# TYPE momo_disk_free_bytes gauge\n")
		fmt.Fprintf(w, "momo_disk_free_bytes %d\n", free)
	} else {
		fmt.Fprintf(w, "# HELP momo_disk_used_bytes Disk bytes used in the data dir.\n")
		fmt.Fprintf(w, "# TYPE momo_disk_used_bytes gauge\n")
		fmt.Fprintf(w, "momo_disk_used_bytes 0\n")
		fmt.Fprintf(w, "# HELP momo_disk_free_bytes Disk bytes free in the data dir.\n")
		fmt.Fprintf(w, "# TYPE momo_disk_free_bytes gauge\n")
		fmt.Fprintf(w, "momo_disk_free_bytes 0\n")
	}
}

// writeClusterMetrics emits R5 phase 3 P2P/cluster gauges from the optional
// clusterStatsProvider (scrape-time only). When p2p is disabled the provider is
// nil and gauges are emitted as 0 per the spec's GIVEN p2p enabled contract.
func (m *MetricsCollector) writeClusterMetrics(w io.Writer) {
	cp := m.clusterOverride
	peers, alive, suspect, offline, latency, leases := 0, 0, 0, 0, 0.0, 0
	if cp != nil {
		peers = cp.PeerCount()
		alive = cp.PeerStateCount(peerStateAlive)
		suspect = cp.PeerStateCount(peerStateSuspect)
		offline = cp.PeerStateCount(peerStateOffline)
		latency = cp.AvgPingLatencySeconds()
		leases = cp.ActiveLeases()
	}
	queries, timeouts := uint64(0), uint64(0)
	if cp != nil {
		queries, timeouts = cp.ScatterCounters()
	}

	fmt.Fprintf(w, "# HELP momo_cluster_peers Number of known peers.\n")
	fmt.Fprintf(w, "# TYPE momo_cluster_peers gauge\n")
	fmt.Fprintf(w, "momo_cluster_peers %d\n", peers)
	if aMax, aMin, aSamples, aOK := metrics.AdaptiveGauges(); aOK {
		writeAdaptiveGauges(w, aMax, aMin, aSamples)
	}
	fmt.Fprintf(w, "# HELP momo_swim_alive_count Number of ALIVE peers.\n")
	fmt.Fprintf(w, "# TYPE momo_swim_alive_count gauge\n")
	fmt.Fprintf(w, "momo_swim_alive_count %d\n", alive)
	fmt.Fprintf(w, "# HELP momo_swim_suspect_count Number of SUSPECT peers.\n")
	fmt.Fprintf(w, "# TYPE momo_swim_suspect_count gauge\n")
	fmt.Fprintf(w, "momo_swim_suspect_count %d\n", suspect)
	fmt.Fprintf(w, "# HELP momo_swim_offline_count Number of OFFLINE peers.\n")
	fmt.Fprintf(w, "# TYPE momo_swim_offline_count gauge\n")
	fmt.Fprintf(w, "momo_swim_offline_count %d\n", offline)
	fmt.Fprintf(w, "# HELP momo_swim_ping_latency_seconds Mean SWIM ping latency (EWMA RTT).\n")
	fmt.Fprintf(w, "# TYPE momo_swim_ping_latency_seconds gauge\n")
	fmt.Fprintf(w, "momo_swim_ping_latency_seconds %.7f\n", latency)
	fmt.Fprintf(w, "# HELP momo_leases_active Number of active leases held.\n")
	fmt.Fprintf(w, "# TYPE momo_leases_active gauge\n")
	fmt.Fprintf(w, "momo_leases_active %d\n", leases)
	fmt.Fprintf(w, "# HELP momo_scatter_queries_total Total scatter-gather queries.\n")
	fmt.Fprintf(w, "# TYPE momo_scatter_queries_total counter\n")
	fmt.Fprintf(w, "momo_scatter_queries_total %d\n", queries)
	fmt.Fprintf(w, "# HELP momo_scatter_timeout_total Total scatter-gather timeout expiries.\n")
	fmt.Fprintf(w, "# TYPE momo_scatter_timeout_total counter\n")
	fmt.Fprintf(w, "momo_scatter_timeout_total %d\n", timeouts)

	handoffs, quorumLoss := uint64(0), uint64(0)
	if cp != nil {
		handoffs, quorumLoss = cp.CoordinationCounters()
	}
	_, _ = fmt.Fprintf(w, "# HELP momo_coordination_handoffs_total Proactive lease resignations due to local unhealthiness.\n")
	_, _ = fmt.Fprintf(w, "# TYPE momo_coordination_handoffs_total counter\n")
	_, _ = fmt.Fprintf(w, "momo_coordination_handoffs_total %d\n", handoffs)
	_, _ = fmt.Fprintf(w, "# HELP momo_coordination_quorum_loss_total Times coordination was suspended because cluster quorum was lost.\n")
	_, _ = fmt.Fprintf(w, "# TYPE momo_coordination_quorum_loss_total counter\n")
	_, _ = fmt.Fprintf(w, "momo_coordination_quorum_loss_total %d\n", quorumLoss)

	// R11 (#1261): peer load as published over gossip. Absent until a report
	// arrives, which is why the samples are emitted per peer rather than as a
	// zero-valued total.
	if cp != nil {
		_, _ = fmt.Fprintf(w, "# HELP momo_peer_disk_used_percent Peer data-disk utilisation as last reported.\n")
		_, _ = fmt.Fprintf(w, "# TYPE momo_peer_disk_used_percent gauge\n")
		for _, load := range cp.PeerLoads() {
			_, _ = fmt.Fprintf(w, "momo_peer_disk_used_percent{peer=\"%d\"} %d\n", load.PeerID, load.DiskUsedPercent)
		}
		_, _ = fmt.Fprintf(w, "# HELP momo_peer_inflight_requests Peer in-flight requests as last reported.\n")
		_, _ = fmt.Fprintf(w, "# TYPE momo_peer_inflight_requests gauge\n")
		for _, load := range cp.PeerLoads() {
			_, _ = fmt.Fprintf(w, "momo_peer_inflight_requests{peer=\"%d\"} %d\n", load.PeerID, load.InflightRequests)
		}
	}
}

// writeLatencyHistograms emits R5 phase 4 opt-in histograms as Prometheus
// summarize format when enabled; otherwise emits documented-empty samples.
func (m *MetricsCollector) writeLatencyHistograms(w io.Writer) {
	enabled := m.histogramsEnabled.Load()
	if !enabled {
		fmt.Fprintf(w, "# HELP momo_request_latency_seconds Request latency by operation (opt-in).\n")
		fmt.Fprintf(w, "# TYPE momo_request_latency_seconds histogram\n")
		fmt.Fprintf(w, "momo_request_latency_seconds_bucket{operation=\"upload\",le=\"+Inf\"} 0\n")
		fmt.Fprintf(w, "momo_request_latency_seconds_count{operation=\"upload\"} 0\n")
		fmt.Fprintf(w, "momo_request_latency_seconds_sum{operation=\"upload\"} 0\n")
		fmt.Fprintf(w, "# HELP momo_replication_latency_seconds Replication latency (opt-in).\n")
		fmt.Fprintf(w, "# TYPE momo_replication_latency_seconds histogram\n")
		fmt.Fprintf(w, "momo_replication_latency_seconds_bucket{le=\"+Inf\"} 0\n")
		fmt.Fprintf(w, "momo_replication_latency_seconds_count 0\n")
		fmt.Fprintf(w, "momo_replication_latency_seconds_sum 0\n")
		return
	}

	fmt.Fprintf(w, "# HELP momo_request_latency_seconds Request latency by operation.\n")
	fmt.Fprintf(w, "# TYPE momo_request_latency_seconds histogram\n")
	// Aggregate operations: all request buckets share the combined count/sum;
	// the operation tag is present on the sample for dashboard filtering.
	for _, op := range []string{"upload", "download", "delete", "list"} {
		buckets, count, sum := m.requestLatency.samples()
		for i, le := range histogramBucketsSeconds {
			fmt.Fprintf(w, "momo_request_latency_seconds_bucket{operation=\"%s\",le=\"%g\"} %d\n", op, le, buckets[i])
		}
		fmt.Fprintf(w, "momo_request_latency_seconds_bucket{operation=\"%s\",le=\"+Inf\"} %d\n", op, count)
		fmt.Fprintf(w, "momo_request_latency_seconds_sum{operation=\"%s\"} %g\n", op, sum)
		fmt.Fprintf(w, "momo_request_latency_seconds_count{operation=\"%s\"} %d\n", op, count)
	}

	fmt.Fprintf(w, "# HELP momo_replication_latency_seconds Replication latency.\n")
	fmt.Fprintf(w, "# TYPE momo_replication_latency_seconds histogram\n")
	buckets, count, sum := m.replicationLat.samples()
	for i, le := range histogramBucketsSeconds {
		fmt.Fprintf(w, "momo_replication_latency_seconds_bucket{le=\"%g\"} %d\n", le, buckets[i])
	}
	fmt.Fprintf(w, "momo_replication_latency_seconds_bucket{le=\"+Inf\"} %d\n", count)
	fmt.Fprintf(w, "momo_replication_latency_seconds_sum %g\n", sum)
	fmt.Fprintf(w, "momo_replication_latency_seconds_count %d\n", count)
}

func (m *MetricsCollector) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	m.writeMetrics(w)
}

// detailedHealthHandler returns a JSON response with detailed cluster health information.
// This includes node state, disk usage, peer health, replication status, and scrub status.
func (m *MetricsCollector) detailedHealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	type ScrubHealth struct {
		Running    bool   `json:"running"`
		LastRun    int64  `json:"last_run_unix_nano"`
		ErrorCount uint64 `json:"error_count"`
	}

	type DetailedHealthResponse struct {
		NodeID                  int         `json:"node_id"`
		UptimeSeconds           float64     `json:"uptime_seconds"`
		State                   string      `json:"state"`
		DiskUsedBytes           uint64      `json:"disk_used_bytes"`
		DiskFreeBytes           uint64      `json:"disk_free_bytes"`
		DiskPercentUsed         float64     `json:"disk_percent_used"`
		PeersAlive              int         `json:"peers_alive"`
		PeersSuspect            int         `json:"peers_suspect"`
		PeersOffline            int         `json:"peers_offline"`
		ReplicationMode         string      `json:"replication_mode"`
		PendingReplicationCount int         `json:"pending_replication_count"`
		ScrubStatus             ScrubHealth `json:"scrub_status"`
		LeasesActive            int         `json:"leases_active"`
		ScatterQueriesPending   uint64      `json:"scatter_queries_pending"`
		// LearnedState is omitted when no provider is installed (#1287).
		LearnedState *LearnedState `json:"learned_state,omitempty"`
	}

	uptime := time.Since(m.startTime).Seconds()

	// Get disk usage
	diskUsed, diskFree, diskPct := uint64(0), uint64(0), 0.0
	if m.storageOverride != nil {
		if used, free, err := m.storageOverride.GetDiskUsage(); err == nil {
			diskUsed = used
			diskFree = free
			if diskUsed+diskFree > 0 {
				diskPct = float64(diskUsed) / float64(diskUsed+diskFree) * 100
			}
		}
	}

	// Get peer counts
	peersAlive, peersSuspect, peersOffline := 0, 0, 0
	if m.clusterOverride != nil {
		peersAlive = m.clusterOverride.PeerStateCount(0)
		peersSuspect = m.clusterOverride.PeerStateCount(1)
		peersOffline = m.clusterOverride.PeerStateCount(2)
	}

	// Get replication status
	repMode, pendingRep := "unknown", 0
	if m.clusterOverride != nil {
		if rp, ok := m.clusterOverride.(interface{ GetReplicationStatus() (string, int) }); ok {
			repMode, pendingRep = rp.GetReplicationStatus()
		}
	}

	// Get scrub status
	scrubRunning, scrubLastRun, scrubErrors := false, int64(0), uint64(0)
	if m.storageOverride != nil {
		if rs, ok := m.storageOverride.(interface{ ScrubStatus() (bool, int64, uint64) }); ok {
			scrubRunning, scrubLastRun, scrubErrors = rs.ScrubStatus()
		}
	}

	// Get leases active
	leasesActive := 0
	if m.storageOverride != nil {
		if ls, ok := m.storageOverride.(interface{ ActiveLeases() int }); ok {
			leasesActive = ls.ActiveLeases()
		}
	}
	_ = leasesActive

	// Get scatter queries pending
	scatterPending := uint64(0)
	if m.clusterOverride != nil {
		if sq, ok := m.clusterOverride.(interface{ ScatterCounters() (uint64, uint64) }); ok {
			_, scatterPending = sq.ScatterCounters()
		}
	}

	// Determine state
	state := "healthy"
	if diskFree == 0 || diskPct >= 99.0 {
		state = "critical"
	} else if peersSuspect > 0 || diskPct > 90.0 {
		state = "degraded"
	}

	resp := DetailedHealthResponse{
		NodeID:                  0, // Will be filled by server
		UptimeSeconds:           uptime,
		State:                   state,
		DiskUsedBytes:           diskUsed,
		DiskFreeBytes:           diskFree,
		DiskPercentUsed:         diskPct,
		PeersAlive:              peersAlive,
		PeersSuspect:            peersSuspect,
		PeersOffline:            peersOffline,
		ReplicationMode:         repMode,
		LearnedState:            m.learnedStateNow(),
		PendingReplicationCount: pendingRep,
		ScrubStatus: struct {
			Running    bool   `json:"running"`
			LastRun    int64  `json:"last_run_unix_nano"`
			ErrorCount uint64 `json:"error_count"`
		}{
			Running:    scrubRunning,
			LastRun:    scrubLastRun,
			ErrorCount: scrubErrors,
		},
		LeasesActive:          0,
		ScatterQueriesPending: scatterPending,
	}

	// We need the node ID - get it from storage if available
	if m.storageOverride != nil {
		// NodeID would be determined by the server
	}

	_ = json.NewEncoder(w).Encode(resp)
}

// StartMetricsServer starts an HTTP server exposing Prometheus metrics on the given
// host and port (empty host binds all interfaces). It runs in a background goroutine
// and returns immediately. The server is shut down when the provided context is canceled.
// requireAdmin gates a read-only admin endpoint: open when no token is
// configured (backward compatible), bearer-protected otherwise.
func requireAdmin(h http.HandlerFunc, adminToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if adminToken != "" && !bearerAuthorized(r, adminToken) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

// requireMutationAuth gates a mutating endpoint: a configured token is
// mandatory, and without one the request is refused unless the listener is
// bound to loopback. Never leaves an off-loopback mutation path open.
func requireMutationAuth(h http.HandlerFunc, adminToken, host string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if adminToken != "" {
			if !bearerAuthorized(r, adminToken) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			h(w, r)
			return
		}
		if !isLoopbackHost(host) {
			http.Error(w, "Forbidden: configure [metrics] admin_token to enable this endpoint on a non-loopback bind", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

// bearerAuthorized compares the request's bearer token in constant time. The
// presented value is never logged.
func bearerAuthorized(r *http.Request, token string) bool {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(auth[len(prefix):]), []byte(token)) == 1
}

// isLoopbackHost reports whether a bind host only accepts local connections.
// An empty host means "all interfaces" and is therefore not loopback.
func isLoopbackHost(host string) bool {
	h := strings.TrimSpace(host)
	if h == "" || h == "0.0.0.0" || h == "::" || h == "[::]" {
		return false
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	if ip := net.ParseIP(strings.Trim(h, "[]")); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// learnedStateNow renders the learned-state block, tolerating a panicking
// provider (Rule 37): health reporting must never take the daemon down.
func (m *MetricsCollector) learnedStateNow() (ls *LearnedState) {
	if m.learnedState == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in learned-state provider: %v (errno=%d)", r, syscall.EIO)
			ls = nil
		}
	}()
	return m.learnedState()
}

func StartMetricsServer(ctx context.Context, host string, port int, collector *MetricsCollector, reloadFn func(), adminToken string) {
	if port <= 0 {
		return
	}

	mux := http.NewServeMux()
	// /health is always open: orchestrators must be able to probe liveness.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("/metrics", requireAdmin(collector.handler, adminToken))
	mux.HandleFunc("/health/detailed", requireAdmin(collector.detailedHealthHandler, adminToken))
	if reloadFn != nil {
		reload := func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			reloadFn()
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Secrets reloaded\n"))
		}
		// Mutating endpoint: with no token configured it is only served on a
		// loopback bind, so an exposed listener can never mutate key material.
		mux.HandleFunc("/reload-secrets", requireMutationAuth(reload, adminToken, host))
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("Failed to start metrics server on port %d: %v", port, err)
		return
	}

	// 🛡️ Sentinel (Rule 24): bound every phase so a slow or idle client cannot
	// hold a connection open indefinitely (Slowloris / resource exhaustion).
	srv := &http.Server{
		Handler:           securityHeadersMiddleware(mux),
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		defer common.RecoverClose("metrics server serve", srv)
		log.Printf("Prometheus metrics server listening on :%d/metrics", port)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("Metrics server error: %v", err)
		}
	}()

	go func() {
		defer common.RecoverClose("metrics server shutdown", srv)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
}

// writeAdaptiveGauges emits the learned threshold gauges. Write errors are
// ignored deliberately: the scrape response is already streaming, and the
// handler cannot retry a partially written body.
func writeAdaptiveGauges(w io.Writer, upper, lower float64, samples int) {
	fmt.Fprintf(w, "# HELP momo_adaptive_max_threshold Learned upper utilisation threshold (percent).\n")       //nolint:errcheck
	fmt.Fprintf(w, "# TYPE momo_adaptive_max_threshold gauge\n")                                                //nolint:errcheck
	fmt.Fprintf(w, "momo_adaptive_max_threshold %.0f\n", upper)                                                 //nolint:errcheck
	fmt.Fprintf(w, "# HELP momo_adaptive_min_threshold Learned lower utilisation threshold (percent).\n")       //nolint:errcheck
	fmt.Fprintf(w, "# TYPE momo_adaptive_min_threshold gauge\n")                                                //nolint:errcheck
	fmt.Fprintf(w, "momo_adaptive_min_threshold %.0f\n", lower)                                                 //nolint:errcheck
	fmt.Fprintf(w, "# HELP momo_adaptive_threshold_samples Observations collected by the threshold learner.\n") //nolint:errcheck
	fmt.Fprintf(w, "# TYPE momo_adaptive_threshold_samples gauge\n")                                            //nolint:errcheck
	fmt.Fprintf(w, "momo_adaptive_threshold_samples %d\n", samples)                                             //nolint:errcheck
}
