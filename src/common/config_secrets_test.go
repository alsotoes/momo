package common

import (
	"testing"
	"time"

	"gopkg.in/ini.v1"
)

func iniFile(t *testing.T, body string) *ini.File {
	t.Helper()
	f, err := ini.Load([]byte(body))
	if err != nil {
		t.Fatalf("ini.Load: %v", err)
	}
	return f
}

func TestLoadSecretsConfig_Full(t *testing.T) {
	f := iniFile(t, `
[secrets]
enabled = true
sources = env,file
rotation_interval = 24h
rotation_grace_period = 1h
[secrets.env]
prefix = MOMO_
[secrets.file]
path = conf/momo.conf
`)

	cfg, err := loadSecretsConfig(f)
	if err != nil {
		t.Fatalf("loadSecretsConfig: %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("expected Enabled=true")
	}
	if cfg.RotationInterval != 24*time.Hour {
		t.Fatalf("RotationInterval = %v, want 24h", cfg.RotationInterval)
	}
	if cfg.RotationGracePeriod != time.Hour {
		t.Fatalf("RotationGracePeriod = %v, want 1h", cfg.RotationGracePeriod)
	}
	if len(cfg.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(cfg.Sources))
	}
	if got := cfg.SourceConfigs[SecretsSourceEnv].EnvPrefix; got != "MOMO_" {
		t.Fatalf("env prefix = %q, want MOMO_", got)
	}
	if got := cfg.SourceConfigs[SecretsSourceFile].FilePath; got != "conf/momo.conf" {
		t.Fatalf("file path = %q, want conf/momo.conf", got)
	}
}

func TestLoadSecretsConfig_Defaults(t *testing.T) {
	f := iniFile(t, "[secrets]\nenabled = true\nsources = env,file\n")
	cfg, err := loadSecretsConfig(f)
	if err != nil {
		t.Fatalf("loadSecretsConfig: %v", err)
	}
	if cfg.RotationGracePeriod != 24*time.Hour {
		t.Fatalf("expected default 24h grace, got %v", cfg.RotationGracePeriod)
	}
	if got := cfg.SourceConfigs[SecretsSourceEnv].EnvPrefix; got != "MOMO_" {
		t.Fatalf("default env prefix = %q, want MOMO_", got)
	}
	if got := cfg.SourceConfigs[SecretsSourceFile].FilePath; got != "conf/momo.conf" {
		t.Fatalf("default file path = %q, want conf/momo.conf", got)
	}
}

func TestLoadSecretsConfig_AllSources(t *testing.T) {
	f := iniFile(t, `
[secrets]
sources = vault,aws-sm,gcp-sm,azure-kv
[secrets.vault]
address = http://v:8200
token = t
path = p
mount = m
[secrets.aws-sm]
region = us-east-1
secret_name = s
[secrets.gcp-sm]
project_id = proj
secret_id = sid
version = 3
[secrets.azure-kv]
vault_url = https://kv/
secret_name = s
version = 2
`)
	cfg, err := loadSecretsConfig(f)
	if err != nil {
		t.Fatalf("loadSecretsConfig: %v", err)
	}
	if len(cfg.Sources) != 4 {
		t.Fatalf("expected 4 sources, got %d", len(cfg.Sources))
	}
	if cfg.SourceConfigs[SecretsSourceVault].VaultAddress != "http://v:8200" {
		t.Fatalf("vault address = %q", cfg.SourceConfigs[SecretsSourceVault].VaultAddress)
	}
	if cfg.SourceConfigs[SecretsSourceAWS].AWSRegion != "us-east-1" {
		t.Fatalf("aws region = %q", cfg.SourceConfigs[SecretsSourceAWS].AWSRegion)
	}
	if cfg.SourceConfigs[SecretsSourceGCP].GCPVersion != "3" {
		t.Fatalf("gcp version = %q", cfg.SourceConfigs[SecretsSourceGCP].GCPVersion)
	}
	if cfg.SourceConfigs[SecretsSourceAzure].AzureVersion != "2" {
		t.Fatalf("azure version = %q", cfg.SourceConfigs[SecretsSourceAzure].AzureVersion)
	}
}

func TestLoadSecretsConfig_NoSection(t *testing.T) {
	f := iniFile(t, "[global]\ndebug = false\n")
	cfg, err := loadSecretsConfig(f)
	if err != nil {
		t.Fatalf("loadSecretsConfig (no section): %v", err)
	}
	if cfg.Enabled {
		t.Fatal("expected disabled config when [secrets] is absent")
	}
}

func TestLoadSecretsConfig_Errors(t *testing.T) {
	for _, body := range []string{
		"[secrets]\nrotation_interval = 90d\n",
		"[secrets]\nrotation_grace_period = 90d\n",
		"[secrets]\nsources = bogus\n",
	} {
		if _, err := loadSecretsConfig(iniFile(t, body)); err == nil {
			t.Fatalf("expected error for config:\n%s", body)
		}
	}
}

func TestLoadAuditConfig(t *testing.T) {
	f := iniFile(t, "[audit]\nenabled = true\nretention_days = 30\n")
	sec, _ := f.GetSection("audit")
	cfg, err := loadAuditConfig(sec)
	if err != nil {
		t.Fatalf("loadAuditConfig: %v", err)
	}
	if !cfg.Enabled || cfg.RetentionDays != 30 {
		t.Fatalf("unexpected audit config: %+v", cfg)
	}

	// retention_days < 1 is rejected.
	f2 := iniFile(t, "[audit]\nretention_days = 0\n")
	sec2, _ := f2.GetSection("audit")
	if _, err := loadAuditConfig(sec2); err == nil {
		t.Fatal("expected error for retention_days=0")
	}
}
