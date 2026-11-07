package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRebalanceConfig(t *testing.T, section string) string {
	t.Helper()
	tmpDir := t.TempDir()
	tmpfile := filepath.Join(tmpDir, "momo.conf")
	if err := os.WriteFile(tmpfile, []byte(validConfig+"\n[rebalance]\n"+section), 0666); err != nil {
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
