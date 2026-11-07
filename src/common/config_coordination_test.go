package common

import (
	"errors"
	"syscall"
	"testing"

	"gopkg.in/ini.v1"
)

func coordinationSection(t *testing.T, body string) *ini.Section {
	t.Helper()
	f := iniFile(t, "[coordination_failover]\n"+body)
	sec, err := f.GetSection("coordination_failover")
	if err != nil {
		t.Fatalf("GetSection: %v", err)
	}
	return sec
}

func TestLoadCoordinationFailoverConfigDefaults(t *testing.T) {
	cfg, err := loadCoordinationFailoverConfig(coordinationSection(t, "enabled=true\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if cfg.Interval != 10 || cfg.CPUThreshold != 90 || cfg.MemoryThreshold != 90 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.GoroutineThreshold != 100000 || cfg.Cooldown != 60 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadCoordinationFailoverConfigOverrides(t *testing.T) {
	cfg, err := loadCoordinationFailoverConfig(coordinationSection(t, `
enabled=true
interval=5
cpu_threshold=75
memory_threshold=80
goroutine_threshold=50000
cooldown=30
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Interval != 5 || cfg.CPUThreshold != 75 || cfg.MemoryThreshold != 80 {
		t.Fatalf("unexpected overrides: %+v", cfg)
	}
	if cfg.GoroutineThreshold != 50000 || cfg.Cooldown != 30 {
		t.Fatalf("unexpected overrides: %+v", cfg)
	}
}

func TestLoadCoordinationFailoverConfigRejectsBadValues(t *testing.T) {
	cases := map[string]string{
		"zero interval":       "interval=0\n",
		"cpu over 100":        "cpu_threshold=101\n",
		"negative cpu":        "cpu_threshold=-1\n",
		"memory over 100":     "memory_threshold=101\n",
		"negative goroutines": "goroutine_threshold=-1\n",
		"negative cooldown":   "cooldown=-1\n",
	}
	for name, body := range cases {
		if _, err := loadCoordinationFailoverConfig(coordinationSection(t, body)); err == nil {
			t.Fatalf("%s: expected an error", name)
		} else if !errors.Is(err, syscall.EINVAL) {
			t.Fatalf("%s: want EINVAL, got %v", name, err)
		}
	}
}
