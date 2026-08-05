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
auth_token = a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6 # notsecret
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
			name:          "Missing auth_token",
			content:       strings.Replace(validConfig, "auth_token = a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6 # notsecret", "", 1),
			expectedError: "failed to load [global] section: 'auth_token' is missing or empty",
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
		{
			name:          "Zero fallback_interval",
			content:       strings.Replace(validConfig, "fallback_interval = 30", "fallback_interval = 0", 1),
			expectedError: "failed to load [metrics] section: 'fallback_interval' must be positive",
		},
		{
			name:          "Negative fallback_interval",
			content:       strings.Replace(validConfig, "fallback_interval = 30", "fallback_interval = -5", 1),
			expectedError: "failed to load [metrics] section: 'fallback_interval' must be positive",
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

func TestGetConfig_FileErrors(t *testing.T) {
	// 1. Non-existent file
	_, err := GetConfig("nonexistent-config.conf")
	if err == nil {
		t.Errorf("Expected error for non-existent file, got nil")
	}

	// 2. Directory as path
	tmpDir := t.TempDir()
	_, err = GetConfig(tmpDir)
	if err == nil {
		t.Errorf("Expected error for directory path, got nil")
	}
}

func TestGetConfig_ClientSideReplicationModes_Default(t *testing.T) {
	// When client_side_replication_modes is absent, it should default to [3]
	tmpDir := t.TempDir()
	tmpfile := filepath.Join(tmpDir, "momo.conf")
	if err := os.WriteFile(tmpfile, []byte(validConfig), 0666); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	config, err := GetConfig(tmpfile)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	expected := []int{ReplicationPrimarySplay}
	if !reflect.DeepEqual(config.Global.ClientSideReplicationModes, expected) {
		t.Errorf("Expected default ClientSideReplicationModes %v, got %v", expected, config.Global.ClientSideReplicationModes)
	}
}

func TestGetConfig_ClientSideReplicationModes_Explicit(t *testing.T) {
	configContent := `
[global]
debug = true
auth_token = a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6 # notsecret
replication_order = 4,3,2,1
client_side_replication_modes = 3,4
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

	tmpDir := t.TempDir()
	tmpfile := filepath.Join(tmpDir, "momo.conf")
	if err := os.WriteFile(tmpfile, []byte(configContent), 0666); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	config, err := GetConfig(tmpfile)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	expected := []int{3, 4}
	if !reflect.DeepEqual(config.Global.ClientSideReplicationModes, expected) {
		t.Errorf("Expected ClientSideReplicationModes %v, got %v", expected, config.Global.ClientSideReplicationModes)
	}
}

const validOPRFShare = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func oprfConfig(daemonExtra string, globalExtra string) string {
	return `
[global]
debug = true
auth_token = a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6 # notsecret
replication_order = 2,3,1
polymorphic_system = true
oprf_enabled = true
` + globalExtra + `
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
oprf_share = ` + validOPRFShare + `
` + daemonExtra + `
`
}

func TestGetConfig_OPRFShares_Valid(t *testing.T) {
	cfg := `
[global]
debug = true
auth_token = a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6 # notsecret
replication_order = 2,3,1
polymorphic_system = true
oprf_enabled = true
oprf_threshold = 1

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
oprf_share = ` + validOPRFShare + `
`
	tmp := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(tmp, []byte(cfg), 0666); err != nil {
		t.Fatal(err)
	}
	config, err := GetConfig(tmp)
	if err != nil {
		t.Fatalf("valid OPRF config rejected: %v", err)
	}
	if !config.Global.OPRFEnabled {
		t.Error("expected OPRFEnabled to be true")
	}
	if config.Daemons[0].OPRFShare != validOPRFShare {
		t.Errorf("OPRFShare mismatch: %q", config.Daemons[0].OPRFShare)
	}
	if config.Daemons[0].OPRFShareIndex != 1 {
		t.Errorf("expected default OPRFShareIndex 1, got %d", config.Daemons[0].OPRFShareIndex)
	}
}

func TestGetConfig_OPRF_ValidationErrors(t *testing.T) {
	testCases := []struct {
		name          string
		content       string
		expectedError string
	}{
		{
			name:          "Missing share when enabled",
			content:       strings.Replace(oprfConfig("", "oprf_threshold = 1\n"), "oprf_share = "+validOPRFShare+"\n", "", 1),
			expectedError: "missing 'oprf_share'",
		},
		{
			name:          "Share wrong length",
			content:       oprfConfig("oprf_share = aabb\n", "oprf_threshold = 1\n"),
			expectedError: "must be 64 hex characters",
		},
		{
			name:          "Share index out of range",
			content:       oprfConfig("oprf_share_index = 5\n", "oprf_threshold = 1\n"),
			expectedError: "out of range",
		},
		{
			name: "Duplicate share index",
			content: oprfConfig("oprf_share_index = 1\n", "oprf_threshold = 1\n") +
				"[daemon.1]\nhost = localhost:8081\nchange_replication = localhost:2223\ndata = /data/1\ndrive = /dev/sda2\noprf_share = " + validOPRFShare + "\noprf_share_index = 1\n",
			expectedError: "duplicate 'oprf_share_index'",
		},
		{
			name:          "Threshold exceeds daemons",
			content:       oprfConfig("", "oprf_threshold = 2\n"),
			expectedError: "exceeds number of daemons",
		},
		{
			name: "Threshold > 1 requires P2P",
			content: oprfConfig("", "oprf_threshold = 2\n") +
				"[daemon.1]\nhost = localhost:8081\nchange_replication = localhost:2223\ndata = /data/1\ndrive = /dev/sda2\noprf_share = " + validOPRFShare + "\n",
			expectedError: "requires [p2p] enabled",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := filepath.Join(t.TempDir(), "momo.conf")
			if err := os.WriteFile(tmp, []byte(tc.content), 0666); err != nil {
				t.Fatal(err)
			}
			_, err := GetConfig(tmp)
			if err == nil {
				t.Fatal("expected an error, got none")
			}
			if !strings.Contains(err.Error(), tc.expectedError) {
				t.Errorf("expected error containing %q, got %q", tc.expectedError, err.Error())
			}
		})
	}
}
