package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfigWithSections builds a config from the shared validConfig plus extra
// sections, so each case exercises one knob.
func writeConfigWithSections(t *testing.T, extra string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "momo.conf")
	if err := os.WriteFile(path, []byte(validConfig+extra), 0666); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestGetConfig_UnimplementedKnobsAreRefused pins the honest posture from #1323:
// a key the daemon does not honour must fail loudly instead of silently doing
// nothing.
func TestGetConfig_UnimplementedKnobsAreRefused(t *testing.T) {
	cases := []struct {
		name  string
		extra string
		key   string
	}{
		{"metadata_ttl", "\n[momofs]\nmetadata_ttl = 30\n", "metadata_ttl"},
		{"lease_timeout", "\n[p2p]\nenabled = true\nlease_timeout = 20\n", "lease_timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := GetConfig(writeConfigWithSections(t, tc.extra))
			if err == nil {
				t.Fatalf("%s must be refused while unimplemented", tc.key)
			}
			if !strings.Contains(err.Error(), tc.key) || !strings.Contains(err.Error(), "not implemented") {
				t.Fatalf("error must name %q as not implemented, got %v", tc.key, err)
			}
			if !strings.Contains(err.Error(), "issues/1323") {
				t.Fatalf("error must point at the tracking issue, got %v", err)
			}
		})
	}
}

// TestGetConfig_MetadataQuorumIsAccepted proves the one key that became real:
// metadata_quorum is parsed into the config that the provider now consumes.
func TestGetConfig_MetadataQuorumIsAccepted(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra string
		want  int
	}{
		{"default majority", "\n[p2p]\nenabled = true\n[momofs]\nenabled = true\nmetadata_replication = 1\n", 0},
		{"explicit one of one", "\n[p2p]\nenabled = true\n[momofs]\nenabled = true\nmetadata_replication = 1\nmetadata_quorum = 1\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := GetConfig(writeConfigWithSections(t, tc.extra))
			if err != nil {
				t.Fatalf("GetConfig: %v", err)
			}
			if cfg.Momofs.MetadataQuorum != tc.want {
				t.Fatalf("MetadataQuorum = %d, want %d", cfg.Momofs.MetadataQuorum, tc.want)
			}
		})
	}
}

// TestGetConfig_UnimplementedKnobDefaultsStayUsable keeps the defaults working:
// the refusal must be triggered by an explicit key, not by the defaults.
func TestGetConfig_UnimplementedKnobDefaultsStayUsable(t *testing.T) {
	cfg, err := GetConfig(writeConfigWithSections(t, "\n[p2p]\nenabled = true\n[momofs]\nenabled = true\nmetadata_replication = 1\n"))
	if err != nil {
		t.Fatalf("defaults must load: %v", err)
	}
	if cfg.Global.WriteQuorum != 1 {
		t.Fatalf("WriteQuorum default = %d, want 1", cfg.Global.WriteQuorum)
	}
	if cfg.Momofs.MetadataTTL != 60*time.Second {
		t.Fatalf("MetadataTTL default = %v, want 60s", cfg.Momofs.MetadataTTL)
	}
	if cfg.P2P.LeaseTimeout != 10 {
		t.Fatalf("LeaseTimeout default = %d, want 10", cfg.P2P.LeaseTimeout)
	}
}
