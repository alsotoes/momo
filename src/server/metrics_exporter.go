package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync/atomic"
	"time"
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
	startTime         time.Time
}

// NewMetricsCollector creates a new MetricsCollector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		startTime: time.Now(),
	}
}

func (m *MetricsCollector) IncConnections()             { m.connectionsTotal.Add(1); m.activeConnections.Add(1) }
func (m *MetricsCollector) DecConnections()             { m.activeConnections.Add(-1) }
func (m *MetricsCollector) IncUploads()                 { m.uploadsTotal.Add(1) }
func (m *MetricsCollector) IncDownloads()               { m.downloadsTotal.Add(1) }
func (m *MetricsCollector) IncDeletes()                 { m.deletesTotal.Add(1) }
func (m *MetricsCollector) IncReplication()             { m.replicationTotal.Add(1) }
func (m *MetricsCollector) IncErrors()                  { m.errorsTotal.Add(1) }
func (m *MetricsCollector) AddBytesUploaded(n uint64)   { m.bytesUploaded.Add(n) }
func (m *MetricsCollector) AddBytesDownloaded(n uint64) { m.bytesDownloaded.Add(n) }

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

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
		log.Printf("AUDIT: os.Hostname() failed: %v", err)
	}
	fmt.Fprintf(w, "# HELP momo_build_info Build information.\n")
	fmt.Fprintf(w, "# TYPE momo_build_info gauge\n")
	fmt.Fprintf(w, "momo_build_info{hostname=\"%s\"} 1\n", hostname)
}

func (m *MetricsCollector) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	m.writeMetrics(w)
}

// StartMetricsServer starts an HTTP server exposing Prometheus metrics on the given port.
// It runs in a background goroutine and returns immediately.
// The server is shut down when the provided context is canceled.
func StartMetricsServer(ctx context.Context, port int, collector *MetricsCollector) {
	if port <= 0 {
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", collector.handler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("Failed to start metrics server on port %d: %v", port, err)
		return
	}

	srv := &http.Server{Handler: mux}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("CRITICAL: Panic recovered in metrics server: %v", r)
			}
		}()
		log.Printf("Prometheus metrics server listening on :%d/metrics", port)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("Metrics server error: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
}
