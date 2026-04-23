package common

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const validConfig = `
[global]
debug = true
replication_order = 2,3,1
polymorphic_system = true

[metrics]
interval = 10
min_threshold = 0.1
max_threshold = 0.9
fallback_interval = 30

[daemon.0]
host = localhost:8080
change_replication = localhost:2222
data = /data/0
drive = /dev/sda1
`

// TestGetConfig_Success tests the successful loading of a valid configuration file.
func TestGetConfig_Success(t *testing.T) {
	tmpDir := t.TempDir()
	tmpfile := filepath.Join(tmpDir, "momo.conf")
	if err := os.WriteFile(tmpfile, []byte(validConfig), 0666); err != nil {
		t.Fatalf("Failed to write to temporary config file: %v", err)
	}

	config, err := GetConfig(tmpfile)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	// Assert Global section
	if !config.Global.Debug {
		t.Error("Expected Global.Debug to be true, but it was false")
	}

	expectedOrder := []int{2, 3, 1}
	if !reflect.DeepEqual(config.Global.ReplicationOrder, expectedOrder) {
		t.Errorf("Expected Global.ReplicationOrder to be %v, but got %v", expectedOrder, config.Global.ReplicationOrder)
	}

	// Assert Metrics section
	if config.Metrics.Interval != 10 {
		t.Errorf("Expected Metrics.Interval to be 10, but got %d", config.Metrics.Interval)
	}

	// Assert Daemons section
	if len(config.Daemons) != 1 {
		t.Fatalf("Expected 1 daemon, but got %d", len(config.Daemons))
	}
	if config.Daemons[0].Host != "localhost:8080" {
		t.Errorf("Expected daemon host to be 'localhost:8080', but got '%s'", config.Daemons[0].Host)
	}
	if config.Daemons[0].ChangeReplication != "localhost:2222" {
		t.Errorf("Expected daemon ChangeReplication to be 'localhost:2222', but got '%s'", config.Daemons[0].ChangeReplication)
	}
}

// TestGetConfig_Failures tests various failure scenarios for GetConfig.
func TestGetConfig_Failures(t *testing.T) {
	testCases := []struct {
		name          string
		content       string
		expectedError string
	}{
		{
			name:          "Missing global section",
			content:       strings.Replace(validConfig, "[global]", "", 1),
			expectedError: "configuration section [global] not found",
		},
		{
			name:          "Missing metrics section",
			content:       strings.Replace(validConfig, "[metrics]", "", 1),
			expectedError: "configuration section [metrics] not found",
		},
		{
			name:          "No daemon sections",
			content:       strings.Split(validConfig, "[daemon.0]")[0],
			expectedError: "no [daemon.*] sections found",
		},
		{
			name:          "Invalid debug value",
			content:       strings.Replace(validConfig, "debug = true", "debug = not-a-bool", 1),
			expectedError: "failed to load [global] section: failed to parse 'debug'",
		},
		{
			name:          "Invalid replication_order value",
			content:       strings.Replace(validConfig, "replication_order = 2,3,1", "replication_order = 2,a,1", 1),
			expectedError: "failed to load [global] section: failed to parse 'replication_order'",
		},
		{
			name:          "Missing host in daemon",
			content:       strings.Replace(validConfig, "host = localhost:8080", "", 1),
			expectedError: "missing 'host' in section [daemon.0]",
		},
		{
			name:          "Missing change_replication in daemon",
			content:       strings.Replace(validConfig, "change_replication = localhost:2222", "", 1),
			expectedError: "missing 'change_replication' in section [daemon.0]",
		},
		{
			name:          "Missing interval in metrics",
			content:       strings.Replace(validConfig, "interval = 10", "", 1),
			expectedError: "failed to load [metrics] section: failed to parse 'interval'",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tmpfile := filepath.Join(tmpDir, "momo.conf")
			if err := os.WriteFile(tmpfile, []byte(tc.content), 0666); err != nil {
				t.Fatalf("Failed to write to temporary config file: %v", err)
			}

			_, err := GetConfig(tmpfile)
			if err == nil {
				t.Fatalf("Expected an error, but got none")
			}

			if !strings.Contains(err.Error(), tc.expectedError) {
				t.Errorf("Expected error to contain '%s', but got '%s'", tc.expectedError, err.Error())
			}
		})
	}
}
