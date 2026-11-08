package common

import (
	"os"
	"path/filepath"
	"testing"
)

// adaptiveBaseConfig is a minimal valid configuration with an [adaptive] section
// appended by each case.
const adaptiveBaseConfig = `[global]
auth_token=test-token-not-secret
replication_factor=2
replication_order=0,2,1
polymorphic_system=false
encryption_enabled=false
encryption_key=0000000000000000000000000000000000000000000000000000000000000000
encryption_tenant=default
data_dir=/tmp/momo-data
debug=false
minimum_durability_factor=1
[daemon.0]
host=127.0.0.1:9000
change_replication=127.0.0.1:9001
data=127.0.0.1:9002
drive=/dev/null
failure_domain=dc1
data_dir=/tmp/momo-data
[metrics]
interval=1000
min_threshold=40
max_threshold=70
fallback_interval=5000
`

// loadAdaptiveCase writes base+extra and returns the loaded configuration.
func loadAdaptiveCase(t *testing.T, extra string) (Configuration, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(path, []byte(adaptiveBaseConfig+extra), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return GetConfig(path)
}

func TestLoadAdaptiveConfigDisabledByDefault(t *testing.T) {
	cfg, err := loadAdaptiveCase(t, "")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if cfg.Adaptive.Enabled {
		t.Fatal("adaptive must be disabled by default")
	}
}

func TestLoadAdaptiveConfigEnabledDefaults(t *testing.T) {
	cfg, err := loadAdaptiveCase(t, "[adaptive]\nenabled=true\n")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if !cfg.Adaptive.Enabled {
		t.Fatal("expected enabled")
	}
	if cfg.Adaptive.Alpha != 0.2 || cfg.Adaptive.MinSamples != 30 || cfg.Adaptive.PersistInterval != 300 {
		t.Fatalf("defaults mismatch: %+v", cfg.Adaptive)
	}
}

func TestLoadAdaptiveConfigRejectsInvalidValues(t *testing.T) {
	cases := map[string]string{
		"alpha too high":     "[adaptive]\nenabled=true\nalpha=1.5\n",
		"min samples zero":   "[adaptive]\nenabled=true\nmin_samples=0\n",
		"persist negative":   "[adaptive]\nenabled=true\npersist_interval=-1\n",
		"alpha not a number": "[adaptive]\nenabled=true\nalpha=nope\n",
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadAdaptiveCase(t, extra); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}
