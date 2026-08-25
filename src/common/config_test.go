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
		{
			name:          "Unsupported storage backend",
			content:       validConfig + "\n[storage]\nbackend = foobar\n",
			expectedError: "failed to load [storage] section: unsupported storage backend",
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

// TestGetConfig_StorageBackends_Valid ensures every supported storage backend is
// accepted at config load time and that the default (missing [storage] section)
// falls back to "local" (issue #649).
func TestGetConfig_StorageBackends_Valid(t *testing.T) {
	for _, backend := range []string{BackendLocal, BackendNFS, BackendS3, BackendRaw} {
		t.Run(backend, func(t *testing.T) {
			tmpDir := t.TempDir()
			tmpfile := filepath.Join(tmpDir, "momo.conf")
			content := validConfig + "\n[storage]\nbackend = " + backend + "\n"
			if err := os.WriteFile(tmpfile, []byte(content), 0666); err != nil {
				t.Fatalf("Failed to write to temporary config file: %v", err)
			}
			config, err := GetConfig(tmpfile)
			if err != nil {
				t.Fatalf("GetConfig failed for backend %q: %v", backend, err)
			}
			if config.Storage.Backend != backend {
				t.Errorf("Expected Storage.Backend to be %q, got %q", backend, config.Storage.Backend)
			}
		})
	}

	t.Run("default local", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpfile := filepath.Join(tmpDir, "momo.conf")
		if err := os.WriteFile(tmpfile, []byte(validConfig), 0666); err != nil {
			t.Fatalf("Failed to write to temporary config file: %v", err)
		}
		config, err := GetConfig(tmpfile)
		if err != nil {
			t.Fatalf("GetConfig failed: %v", err)
		}
		if config.Storage.Backend != BackendLocal {
			t.Errorf("Expected default Storage.Backend to be %q, got %q", BackendLocal, config.Storage.Backend)
		}
	})
}

// TestGetConfig_StorageIntegrity covers the #924 scrub_interval and
// verify_on_read keys, including their defaults.
func TestGetConfig_StorageIntegrity(t *testing.T) {
	mk := func(t *testing.T, storage string) string {
		t.Helper()
		tmpDir := t.TempDir()
		tmpfile := filepath.Join(tmpDir, "momo.conf")
		if err := os.WriteFile(tmpfile, []byte(validConfig+"\n[storage]\n"+storage), 0666); err != nil {
			t.Fatalf("Failed to write config: %v", err)
		}
		return tmpfile
	}

	t.Run("defaults", func(t *testing.T) {
		cfg, err := GetConfig(mk(t, "backend = local\n"))
		if err != nil {
			t.Fatalf("GetConfig failed: %v", err)
		}
		if cfg.Storage.ScrubInterval != 3600 {
			t.Errorf("default ScrubInterval = %d, want 3600", cfg.Storage.ScrubInterval)
		}
		if !cfg.Storage.VerifyOnRead {
			t.Error("default VerifyOnRead should be true")
		}
	})

	t.Run("overrides", func(t *testing.T) {
		cfg, err := GetConfig(mk(t, "backend = local\nscrub_interval = 60\nverify_on_read = false\n"))
		if err != nil {
			t.Fatalf("GetConfig failed: %v", err)
		}
		if cfg.Storage.ScrubInterval != 60 {
			t.Errorf("ScrubInterval = %d, want 60", cfg.Storage.ScrubInterval)
		}
		if cfg.Storage.VerifyOnRead {
			t.Error("VerifyOnRead should be false")
		}
	})

	t.Run("invalid scrub_interval falls back to default", func(t *testing.T) {
		cfg, err := GetConfig(mk(t, "backend = local\nscrub_interval = 0\n"))
		if err != nil {
			t.Fatalf("GetConfig failed: %v", err)
		}
		if cfg.Storage.ScrubInterval != 3600 {
			t.Errorf("invalid ScrubInterval should fall back to 3600, got %d", cfg.Storage.ScrubInterval)
		}
	})
}

// TestGetConfig_S3GatewayCredentials covers the issue #656 dedicated S3 gateway
// SigV4 credential pair validation and parsing.
func TestGetConfig_S3GatewayCredentials(t *testing.T) {
	mk := func(t *testing.T, storage string) string {
		t.Helper()
		tmpDir := t.TempDir()
		tmpfile := filepath.Join(tmpDir, "momo.conf")
		if err := os.WriteFile(tmpfile, []byte(validConfig+"\n[storage]\n"+storage), 0666); err != nil {
			t.Fatalf("Failed to write config: %v", err)
		}
		return tmpfile
	}

	t.Run("pair parsed", func(t *testing.T) {
		cfg, err := GetConfig(mk(t, "backend = local\ns3_server_access_key = AKIAIOSFODNN7EXAMPLE\ns3_server_secret_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\n"))
		if err != nil {
			t.Fatalf("GetConfig failed: %v", err)
		}
		if cfg.Storage.S3ServerAccessKey != "AKIAIOSFODNN7EXAMPLE" {
			t.Errorf("unexpected S3ServerAccessKey %q", cfg.Storage.S3ServerAccessKey)
		}
		if cfg.Storage.S3ServerSecretKey != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
			t.Errorf("unexpected S3ServerSecretKey %q", cfg.Storage.S3ServerSecretKey)
		}
	})

	t.Run("single key rejected", func(t *testing.T) {
		_, err := GetConfig(mk(t, "backend = local\ns3_server_access_key = AKIAIOSFODNN7EXAMPLE\n"))
		if err == nil {
			t.Fatal("expected error when only access key is set")
		}
		if !strings.Contains(err.Error(), "must be configured together") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("overlong key rejected", func(t *testing.T) {
		_, err := GetConfig(mk(t, "backend = local\ns3_server_access_key = AKIAIOSFODNN7EXAMPLE\ns3_server_secret_key = "+strings.Repeat("k", 65)+"\n"))
		if err == nil {
			t.Fatal("expected error for overlong secret key")
		}
		if !strings.Contains(err.Error(), "s3_server_secret_key") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// TestLoadDaemons_MissingFieldsDeterministic ensures that when multiple required
// daemon fields are missing, the reported error is deterministic across runs
// (map iteration order must not influence the chosen field, issue #644).
func TestLoadDaemons_MissingFieldsDeterministic(t *testing.T) {
	// Drop both host and drive from the daemon section.
	content := strings.Replace(validConfig, "host = localhost:8080", "", 1)
	content = strings.Replace(content, "drive = /dev/sda1", "", 1)

	tmpDir := t.TempDir()
	tmpfile := filepath.Join(tmpDir, "momo.conf")
	if err := os.WriteFile(tmpfile, []byte(content), 0666); err != nil {
		t.Fatalf("Failed to write to temporary config file: %v", err)
	}

	var firstErr error
	const iterations = 100
	for i := 0; i < iterations; i++ {
		_, err := GetConfig(tmpfile)
		if err == nil {
			t.Fatalf("Expected an error for missing fields, got none")
		}
		if i == 0 {
			firstErr = err
			continue
		}
		if err.Error() != firstErr.Error() {
			t.Fatalf("Non-deterministic error: iteration %d got %q, wanted %q", i, err.Error(), firstErr.Error())
		}
	}

	// Sorted field order is: change_replication, data, drive, host.
	// "drive" is the alphabetically-first missing field, so it must be reported.
	if !strings.Contains(firstErr.Error(), "missing 'drive' in section [daemon.0]") {
		t.Errorf("Expected deterministic error for alphabetically-first missing field, got %q", firstErr.Error())
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

func TestGetConfig_ClientSideReplicationModes_DefaultNotAliased(t *testing.T) {
	// Mutating the returned slice must not corrupt the package-level default
	// shared with subsequent GetConfig calls (Rule 9 - no alias the shared slice).
	tmpDir := t.TempDir()
	tmpfile := filepath.Join(tmpDir, "momo.conf")
	if err := os.WriteFile(tmpfile, []byte(validConfig), 0666); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	config, err := GetConfig(tmpfile)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	original := []int{ReplicationPrimarySplay}
	config.Global.ClientSideReplicationModes[0] = ReplicationPrimarySplay + 99

	config2, err := GetConfig(tmpfile)
	if err != nil {
		t.Fatalf("second GetConfig failed: %v", err)
	}
	if !reflect.DeepEqual(config2.Global.ClientSideReplicationModes, original) {
		t.Errorf("default slice was aliased: subsequent GetConfig returned %v, want %v",
			config2.Global.ClientSideReplicationModes, original)
	}
	if !reflect.DeepEqual(defaultClientSideReplicationModes, original) {
		t.Errorf("package-level default mutated: got %v, want %v",
			defaultClientSideReplicationModes, original)
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

// TestGetConfig_E2EE_Valid verifies that a 64-hex 'e2ee_key' loads correctly
// with a default key id (issue #780).
func TestGetConfig_E2EE_Valid(t *testing.T) {
	cfg := `
[global]
debug = true
auth_token = a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6 # notsecret
replication_order = 2,3,1
polymorphic_system = true
e2ee_key = 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
e2ee_key_id = my-key

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
	tmp := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(tmp, []byte(cfg), 0666); err != nil {
		t.Fatal(err)
	}
	config, err := GetConfig(tmp)
	if err != nil {
		t.Fatalf("valid E2EE config rejected: %v", err)
	}
	if config.Global.E2EEKey != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Errorf("E2EEKey mismatch: %q", config.Global.E2EEKey)
	}
	if config.Global.E2EEKeyID != "my-key" {
		t.Errorf("E2EEKeyID mismatch: %q", config.Global.E2EEKeyID)
	}
}

// TestGetConfig_E2EE_DefaultKeyID verifies the "default" key id fallback when
// only 'e2ee_key' is provided.
func TestGetConfig_E2EE_DefaultKeyID(t *testing.T) {
	cfg := `
[global]
debug = true
auth_token = a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6 # notsecret
replication_order = 2,3,1
polymorphic_system = true
e2ee_key = 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef

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
	tmp := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(tmp, []byte(cfg), 0666); err != nil {
		t.Fatal(err)
	}
	config, err := GetConfig(tmp)
	if err != nil {
		t.Fatalf("valid E2EE config rejected: %v", err)
	}
	if config.Global.E2EEKeyID != "default" {
		t.Errorf("expected default E2EEKeyID, got %q", config.Global.E2EEKeyID)
	}
}

// TestGetConfig_E2EE_ValidationErrors covers E2EE config validation (issue #780).
func TestGetConfig_E2EE_ValidationErrors(t *testing.T) {
	testCases := []struct {
		name          string
		globalExtra   string
		expectedError string
	}{
		{
			name:          "Key wrong length",
			globalExtra:   "e2ee_key = aabb\n",
			expectedError: "must be 64 hex characters",
		},
		{
			name:          "Key invalid hex",
			globalExtra:   "e2ee_key = " + strings.Repeat("z", 64) + "\n",
			expectedError: "must be valid hex",
		},
		{
			name:          "Mutually exclusive with OPRF",
			globalExtra:   "e2ee_key = 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\noprf_enabled = true\noprf_threshold = 1\n",
			expectedError: "mutually exclusive with OPRF",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			content := `
[global]
debug = true
auth_token = a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6 # notsecret
replication_order = 2,3,1
polymorphic_system = true
` + tc.globalExtra + `
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
			if err := os.WriteFile(tmp, []byte(content), 0666); err != nil {
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

func TestGetConfig_MinimumDurabilityFactor(t *testing.T) {
	write := func(globalKey string) string {
		return strings.Replace(validConfig, "polymorphic_system = true",
			"polymorphic_system = true\n"+globalKey, 1)
	}

	// Valid explicit value parses. validConfig sets replication_factor? It does
	// not, so it defaults to 3 — a floor of 2 is <= 3 and accepted.
	tmp := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(tmp, []byte(write("minimum_durability_factor = 2")), 0666); err != nil {
		t.Fatal(err)
	}
	cfg, err := GetConfig(tmp)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if cfg.Global.MinimumDurabilityFactor != 2 {
		t.Fatalf("expected MinimumDurabilityFactor=2, got %d", cfg.Global.MinimumDurabilityFactor)
	}

	// Absent defaults to 0 (disabled).
	tmp2 := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(tmp2, []byte(validConfig), 0666); err != nil {
		t.Fatal(err)
	}
	cfg2, err := GetConfig(tmp2)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if cfg2.Global.MinimumDurabilityFactor != 0 {
		t.Fatalf("expected default MinimumDurabilityFactor=0, got %d", cfg2.Global.MinimumDurabilityFactor)
	}

	// Negative rejected.
	tmp3 := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(tmp3, []byte(write("minimum_durability_factor = -1")), 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := GetConfig(tmp3); err == nil {
		t.Fatal("expected negative minimum_durability_factor to be rejected")
	}

	// Floor exceeding replication_factor (default 3) rejected.
	tmp4 := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(tmp4, []byte(write("minimum_durability_factor = 4")), 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := GetConfig(tmp4); err == nil {
		t.Fatal("expected floor > replication_factor to be rejected")
	}
}

func TestGetConfig_AuthBackoffDelay(t *testing.T) {
	write := func(globalKey string) string {
		return strings.Replace(validConfig, "polymorphic_system = true",
			"polymorphic_system = true\n"+globalKey, 1)
	}

	// Explicit value parses into the struct.
	tmp := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(tmp, []byte(write("auth_backoff_delay = 250")), 0666); err != nil {
		t.Fatal(err)
	}
	cfg, err := GetConfig(tmp)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if cfg.Global.AuthBackoffDelay != 250 {
		t.Fatalf("expected AuthBackoffDelay=250, got %d", cfg.Global.AuthBackoffDelay)
	}

	// Absent defaults to 0 (disabled).
	tmp2 := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(tmp2, []byte(validConfig), 0666); err != nil {
		t.Fatal(err)
	}
	cfg2, err := GetConfig(tmp2)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if cfg2.Global.AuthBackoffDelay != 0 {
		t.Fatalf("expected default AuthBackoffDelay=0, got %d", cfg2.Global.AuthBackoffDelay)
	}

	// Negative value is rejected.
	tmp3 := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(tmp3, []byte(write("auth_backoff_delay = -5")), 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := GetConfig(tmp3); err == nil {
		t.Fatal("expected negative auth_backoff_delay to be rejected")
	}
}

func TestGetConfig_GossipFanout(t *testing.T) {
	// Append a [p2p] section to validConfig.
	base := validConfig + "\n[p2p]\nenabled = true\n"

	// Absent fanout defaults to 0 (adaptive).
	tmp := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(tmp, []byte(base), 0666); err != nil {
		t.Fatal(err)
	}
	cfg, err := GetConfig(tmp)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if !cfg.P2P.Enabled {
		t.Fatal("expected p2p enabled")
	}
	if cfg.P2P.Fanout != 0 {
		t.Fatalf("expected default adaptive fanout 0, got %d", cfg.P2P.Fanout)
	}

	// Explicit positive fanout preserved.
	tmp2 := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(tmp2, []byte(base+"fanout = 3\n"), 0666); err != nil {
		t.Fatal(err)
	}
	cfg2, err := GetConfig(tmp2)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if cfg2.P2P.Fanout != 3 {
		t.Fatalf("expected explicit fanout 3, got %d", cfg2.P2P.Fanout)
	}

	// Negative fanout rejected.
	tmp3 := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(tmp3, []byte(base+"fanout = -1\n"), 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := GetConfig(tmp3); err == nil {
		t.Fatal("expected negative fanout to be rejected")
	}
}
