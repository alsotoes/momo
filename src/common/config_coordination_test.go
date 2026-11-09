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

func loadCoordinationElection(t *testing.T, body string) (ConfigurationCoordination, error) {
	t.Helper()
	f := iniFile(t, "[coordination]\n"+body)
	sec, err := f.GetSection("coordination")
	if err != nil {
		t.Fatalf("GetSection: %v", err)
	}
	return loadCoordinationConfig(sec)
}

func TestLoadCoordinationElectionDefaults(t *testing.T) {
	cfg, err := loadCoordinationElection(t, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("election must be opt-in")
	}
	if cfg.MetricsControllerLeaseTTL != 30 {
		t.Fatalf("ttl = %d, want 30", cfg.MetricsControllerLeaseTTL)
	}
}

func TestLoadCoordinationElectionValues(t *testing.T) {
	cfg, err := loadCoordinationElection(t, "enabled=true\nmetrics_controller_lease_ttl=45\n")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Enabled || cfg.MetricsControllerLeaseTTL != 45 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadCoordinationElectionRejectsNonPositiveTTL(t *testing.T) {
	_, err := loadCoordinationElection(t, "enabled=true\nmetrics_controller_lease_ttl=0\n")
	if !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("expected EINVAL, got %v", err)
	}
}

func metricsSectionForTest(t *testing.T, body string) *ini.Section {
	t.Helper()
	f := iniFile(t, "[metrics]\ninterval=1000\nmin_threshold=0.2\nmax_threshold=0.8\nfallback_interval=60000\n"+body)
	sec, err := f.GetSection("metrics")
	if err != nil {
		t.Fatalf("GetSection: %v", err)
	}
	return sec
}

func TestLoadMetricsConfigAdminTokenDefaultEmpty(t *testing.T) {
	cfg, err := loadMetricsConfig(metricsSectionForTest(t, ""))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AdminToken != "" {
		t.Fatalf("admin_token = %q, want empty", cfg.AdminToken)
	}
}

func TestLoadMetricsConfigAdminTokenAccepted(t *testing.T) {
	cfg, err := loadMetricsConfig(metricsSectionForTest(t, "admin_token=0123456789abcdef\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AdminToken != "0123456789abcdef" {
		t.Fatalf("admin_token = %q", cfg.AdminToken)
	}
}

func TestLoadMetricsConfigAdminTokenTooShort(t *testing.T) {
	_, err := loadMetricsConfig(metricsSectionForTest(t, "admin_token=short\n"))
	if !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("expected EINVAL for a short token, got %v", err)
	}
}

func serverSectionForTest(t *testing.T, body string) *ini.Section {
	t.Helper()
	f := iniFile(t, "[server]\n"+body)
	sec, err := f.GetSection("server")
	if err != nil {
		t.Fatalf("GetSection: %v", err)
	}
	return sec
}

func TestLoadServerConfigDefaults(t *testing.T) {
	cfg, err := loadServerConfig(serverSectionForTest(t, ""))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HandshakeTimeout != 10 || cfg.MetadataDeadline != 60 || cfg.MaxConcurrentConnections != 1000 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadServerConfigOverrides(t *testing.T) {
	cfg, err := loadServerConfig(serverSectionForTest(t, "handshake_timeout=3\nmetadata_deadline=120\nmax_concurrent_connections=250\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HandshakeTimeout != 3 || cfg.MetadataDeadline != 120 || cfg.MaxConcurrentConnections != 250 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadServerConfigRejectsNonPositive(t *testing.T) {
	_, err := loadServerConfig(serverSectionForTest(t, "handshake_timeout=0\n"))
	if !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("expected EINVAL, got %v", err)
	}
}

func TestLoadCoordinationPropagationTimeout(t *testing.T) {
	cfg, err := loadCoordinationElection(t, "propagation_timeout=4\n")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PropagationTimeout != 4 {
		t.Fatalf("propagation_timeout = %d, want 4", cfg.PropagationTimeout)
	}
	_, err = loadCoordinationElection(t, "propagation_timeout=0\n")
	if !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("expected EINVAL for a zero timeout, got %v", err)
	}
}
