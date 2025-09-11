package common

import (
	"os"
	"testing"
)

func TestGetConfig(t *testing.T) {
	// Create a dummy config file for testing
	configFileContent := `
[global]
debug = true
replication_order = primary-splay
polymorphic_system = true

[metrics]
interval = 10
min_threshold = 0.1
max_threshold = 0.9
fallback_interval = 30

[daemon.0]
host = localhost:8080
change_replication = splay
data = /data/0
drive = /dev/sda1

[daemon.1]
host = localhost:8081
change_replication = splay
data = /data/1
drive = /dev/sdb1
`
	if err := os.MkdirAll("conf", 0755); err != nil {
		t.Fatalf("Failed to create conf directory: %v", err)
	}
	dfh, err := os.Create("./conf/momo.conf")
	if err != nil {
		t.Fatalf("Failed to create dummy config file: %v", err)
	}
	defer os.RemoveAll("./conf")

	_, err = dfh.WriteString(configFileContent)
	if err != nil {
		t.Fatalf("Failed to write to dummy config file: %v", err)
	}
	dfh.Close()

	config := GetConfig()

	// Assertions for [global] section
	if !config.Global.Debug {
		t.Error("Expected Global.Debug to be true, but it was false")
	}
	if config.Global.ReplicationOrder != "primary-splay" {
		t.Errorf("Expected Global.ReplicationOrder to be 'primary-splay', but got '%s'", config.Global.ReplicationOrder)
	}
	if !config.Global.PolymorphicSystem {
		t.Error("Expected Global.PolymorphicSystem to be true, but it was false")
	}

	// Assertions for [metrics] section
	if config.Metrics.Interval != 10 {
		t.Errorf("Expected Metrics.Interval to be 10, but got %d", config.Metrics.Interval)
	}
	if config.Metrics.MinThreshold != 0.1 {
		t.Errorf("Expected Metrics.MinThreshold to be 0.1, but got %f", config.Metrics.MinThreshold)
	}
	if config.Metrics.MaxThreshold != 0.9 {
		t.Errorf("Expected Metrics.MaxThreshold to be 0.9, but got %f", config.Metrics.MaxThreshold)
	}
	if config.Metrics.FallbackInterval != 30 {
		t.Errorf("Expected Metrics.FallbackInterval to be 30, but got %d", config.Metrics.FallbackInterval)
	}

	// Assertions for [daemon] sections
	if len(config.Daemons) != 2 {
		t.Fatalf("Expected 2 daemons, but got %d", len(config.Daemons))
	}

	// Daemon 0
	if config.Daemons[0].Host != "localhost:8080" {
		t.Errorf("Expected Daemons[0].Host to be 'localhost:8080', but got '%s'", config.Daemons[0].Host)
	}

	// Daemon 1
	if config.Daemons[1].Host != "localhost:8081" {
		t.Errorf("Expected Daemons[1].Host to be 'localhost:8081', but got '%s'", config.Daemons[1].Host)
	}
}
