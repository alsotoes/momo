package server

import (
	"strings"
	"testing"
)

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
		"momo_connections_total 2":       "connections",
		"momo_active_connections 1":      "active connections",
		"momo_uploads_total 3":           "uploads",
		"momo_bytes_uploaded_total 1024": "bytes uploaded",
		"momo_downloads_total 1":         "downloads",
		"momo_bytes_downloaded_total 512": "bytes downloaded",
		"momo_deletes_total 1":           "deletes",
		"momo_replication_total 2":       "replication",
		"momo_errors_total 1":            "errors",
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

func captureMetricsOutput(mc *MetricsCollector) string {
	var buf strings.Builder
	mc.writeMetrics(&buf)
	return buf.String()
}
