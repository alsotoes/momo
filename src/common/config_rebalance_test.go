package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rebalanceClusterConfig is validConfig plus the cluster settings a shedding
// node needs: two daemons, P2P on, and a replication factor above one.
func rebalanceClusterConfig() string {
	base := strings.Replace(validConfig, "polymorphic_system = true", "polymorphic_system = true\nreplication_factor = 2", 1)
	return base + `
[daemon.1]
host = localhost:8081
change_replication = localhost:2223
data = /data/1
drive = /dev/sdb1

[p2p]
enabled = true
`
}

func writeRebalanceConfig(t *testing.T, section string) string {
	t.Helper()
	tmpDir := t.TempDir()
	tmpfile := filepath.Join(tmpDir, "momo.conf")
	if err := os.WriteFile(tmpfile, []byte(rebalanceClusterConfig()+"\n[rebalance]\n"+section), 0666); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return tmpfile
}

func TestGetConfig_RebalanceDefaults(t *testing.T) {
	cfg, err := GetConfig(writeRebalanceConfig(t, "enabled = true\n"))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if !cfg.Rebalance.Enabled {
		t.Fatal("expected rebalance enabled")
	}
	if cfg.Rebalance.Interval != defaultRebalanceInterval ||
		cfg.Rebalance.DiskHighWater != defaultRebalanceHigh ||
		cfg.Rebalance.DiskLowWater != defaultRebalanceLow ||
		cfg.Rebalance.MaxShedPerCycle != defaultRebalanceMaxShed {
		t.Fatalf("unexpected defaults: %+v", cfg.Rebalance)
	}
}

func TestGetConfig_RebalanceExplicit(t *testing.T) {
	cfg, err := GetConfig(writeRebalanceConfig(t, "enabled = true\ninterval = 120\ndisk_high_water = 90\ndisk_low_water = 60\nmax_shed_per_cycle = 8\nmax_bandwidth_mbps = 50\n"))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	want := ConfigurationRebalance{Enabled: true, Interval: 120, DiskHighWater: 90, DiskLowWater: 60, MaxShedPerCycle: 8, MaxBandwidthMbps: 50}
	if cfg.Rebalance != want {
		t.Fatalf("got %+v want %+v", cfg.Rebalance, want)
	}
}

func TestGetConfig_RebalanceValidation(t *testing.T) {
	cases := map[string]string{
		"zero interval":      "interval = 0\n",
		"high out of range":  "disk_high_water = 0\n",
		"low >= high":        "disk_high_water = 80\ndisk_low_water = 80\n",
		"negative low":       "disk_low_water = -1\n",
		"zero max shed":      "max_shed_per_cycle = 0\n",
		"negative bandwidth": "max_bandwidth_mbps = -1\n",
	}
	for name, section := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := GetConfig(writeRebalanceConfig(t, section))
			if err == nil || !strings.Contains(err.Error(), "rebalance") {
				t.Fatalf("want a [rebalance] validation error, got %v", err)
			}
		})
	}
}

func TestGetConfig_RebalanceAbsentDefaults(t *testing.T) {
	_ = rebalanceClusterConfig()
	tmpDir := t.TempDir()
	tmpfile := filepath.Join(tmpDir, "momo.conf")
	if err := os.WriteFile(tmpfile, []byte(validConfig), 0666); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := GetConfig(tmpfile)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if cfg.Rebalance.Enabled {
		t.Fatal("rebalance must default to disabled")
	}
	if cfg.Rebalance != defaultRebalanceConfig() {
		t.Fatalf("unexpected defaults: %+v", cfg.Rebalance)
	}
}

func TestGetConfig_RebalanceRequiresClusterSettings(t *testing.T) {
	// validConfig has one daemon, P2P off, default replication factor.
	tmpDir := t.TempDir()
	tmpfile := filepath.Join(tmpDir, "momo.conf")
	body := validConfig + "\n[rebalance]\nenabled = true\n"
	if err := os.WriteFile(tmpfile, []byte(body), 0666); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := GetConfig(tmpfile)
	if err == nil || !strings.Contains(err.Error(), "[rebalance]") {
		t.Fatalf("want a rebalance cluster precondition error, got %v", err)
	}
}

func TestValidateRebalanceConfig(t *testing.T) {
	base := func() Configuration {
		var cfg Configuration
		cfg.P2P.Enabled = true
		cfg.Storage.DegradedRead = true
		cfg.Global.ReplicationFactor = 3
		cfg.Daemons = []*Daemon{{Host: "a"}, {Host: "b"}}
		return cfg
	}
	if err := validateRebalanceConfig(base()); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	for name, mutate := range map[string]func(*Configuration){
		"p2p off":       func(c *Configuration) { c.P2P.Enabled = false },
		"degraded off":  func(c *Configuration) { c.Storage.DegradedRead = false },
		"rf=1":          func(c *Configuration) { c.Global.ReplicationFactor = 1 },
		"single daemon": func(c *Configuration) { c.Daemons = c.Daemons[:1] },
	} {
		cfg := base()
		mutate(&cfg)
		if err := validateRebalanceConfig(cfg); err == nil {
			t.Fatalf("%s must be rejected", name)
		}
	}
}
