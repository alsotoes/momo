package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("failed to release reserved port: %v", err)
	}
	return port
}

// httpGetPort retries GET until the async server is listening (or a deadline is
// hit), so tests are deterministic regardless of scheduler timing.
func httpGetPort(t *testing.T, port int, path string) (int, string) {
	t.Helper()
	url := "http://127.0.0.1:" + strconv.Itoa(port) + path
	deadline := time.Now().Add(3 * time.Second)
	for {
		req, err := http.Get(url)
		if err == nil {
			body, rerr := io.ReadAll(req.Body)
			req.Body.Close()
			if rerr != nil {
				t.Fatalf("read body failed: %v", rerr)
			}
			return req.StatusCode, string(body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s on port %d failed after retries: %v", path, port, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestStartMetricsServer_HostPort verifies an explicit host:port bind serves
// /metrics and /health.
func TestStartMetricsServer_HostPort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port := freePort(t)
	mc := NewMetricsCollector()

	done := make(chan struct{})
	go func() { StartMetricsServer(ctx, "127.0.0.1", port, mc); close(done) }()

	// /health 200 + "OK".
	code, body := httpGetPort(t, port, "/health")
	if code != http.StatusOK {
		t.Fatalf("expected /health 200, got %d", code)
	}
	if !strings.Contains(body, "OK") {
		t.Fatalf("expected /health body to contain OK, got %q", body)
	}

	// /metrics 200 + a metric line.
	code, body = httpGetPort(t, port, "/metrics")
	if code != http.StatusOK {
		t.Fatalf("expected /metrics 200, got %d", code)
	}
	if !strings.Contains(body, "momo_connections_total ") {
		t.Fatalf("expected /metrics to contain momo_connections_total, got head %q", body[:min(120, len(body))])
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StartMetricsServer did not return after cancel")
	}
	time.Sleep(50 * time.Millisecond)
	goleak.VerifyNone(t)
}

// TestStartMetricsServer_DistinctSameHostPorts verifies two nodes on the same
// host with distinct ports both serve /metrics (no EADDRINUSE).
func TestStartMetricsServer_DistinctSameHostPorts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p1 := freePort(t)
	p2 := freePort(t)
	for p2 == p1 {
		p2 = freePort(t)
	}

	var wg sync.WaitGroup
	for _, p := range []int{p1, p2} {
		wg.Add(1)
		go func(pp int) { defer wg.Done(); StartMetricsServer(ctx, "127.0.0.1", pp, NewMetricsCollector()) }(p)
	}

	// Both must respond.
	httpGetPort(t, p1, "/metrics")
	httpGetPort(t, p2, "/metrics")

	cancel()
	wg.Wait()
	time.Sleep(50 * time.Millisecond)
	goleak.VerifyNone(t)
}

// TestStartMetricsServer_PortCollisionNoPanic verifies binding an in-use port
// logs an error and returns without panicking.
func TestStartMetricsServer_PortCollisionNoPanic(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() { StartMetricsServer(ctx, "127.0.0.1", port, NewMetricsCollector()); close(done) }()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StartMetricsServer should return quickly on collision")
	}
	goleak.VerifyNone(t)
}

func TestMetricsCollector_Counters(t *testing.T) {
	mc := NewMetricsCollector()

	mc.IncConnections()
	mc.IncConnections()
	mc.DecConnections()
	mc.IncUploads()
	mc.IncUploads()
	mc.IncUploads()
	mc.AddBytesUploaded(1024)
	mc.IncDownloads()
	mc.AddBytesDownloaded(512)
	mc.IncDeletes()
	mc.IncReplication()
	mc.IncReplication()
	mc.IncErrors()

	output := captureMetricsOutput(mc)

	checks := map[string]string{
		"momo_connections_total 2":        "connections",
		"momo_active_connections 1":       "active connections",
		"momo_uploads_total 3":            "uploads",
		"momo_bytes_uploaded_total 1024":  "bytes uploaded",
		"momo_downloads_total 1":          "downloads",
		"momo_bytes_downloaded_total 512": "bytes downloaded",
		"momo_deletes_total 1":            "deletes",
		"momo_replication_total 2":        "replication",
		"momo_errors_total 1":             "errors",
	}

	for expected, name := range checks {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected metrics to contain %q (%s), got output:\n%s", expected, name, output)
		}
	}
}

func TestMetricsCollector_ZeroValue(t *testing.T) {
	mc := NewMetricsCollector()

	output := captureMetricsOutput(mc)

	if !strings.Contains(output, "momo_connections_total 0") {
		t.Errorf("Expected zero connections, got:\n%s", output)
	}
	if !strings.Contains(output, "momo_downloads_total 0") {
		t.Errorf("Expected zero downloads, got:\n%s", output)
	}
	if !strings.Contains(output, "momo_deletes_total 0") {
		t.Errorf("Expected zero deletes, got:\n%s", output)
	}
	if !strings.Contains(output, "momo_replication_total 0") {
		t.Errorf("Expected zero replication, got:\n%s", output)
	}
}

func TestMetricsCollector_HostnameCached(t *testing.T) {
	mc := NewMetricsCollector()

	if mc.hostname == "" {
		t.Errorf("Expected cached hostname to be non-empty, got %q", mc.hostname)
	}

	out1 := captureMetricsOutput(mc)
	out2 := captureMetricsOutput(mc)

	want := `momo_build_info{hostname="` + mc.hostname + `"} 1`
	for name, output := range map[string]string{"first": out1, "second": out2} {
		if !strings.Contains(output, want) {
			t.Errorf("Expected %s scrape to contain %q, got output:\n%s", name, want, output)
		}
	}
}

func captureMetricsOutput(mc *MetricsCollector) string {
	var buf strings.Builder
	mc.writeMetrics(&buf)
	return buf.String()
}
