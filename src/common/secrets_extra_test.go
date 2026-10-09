package common

import (
	"context"
	"errors"
	"testing"
)

type closeErrProvider struct{}

func (closeErrProvider) Init(context.Context, SecretsSourceConfig) error { return nil }
func (closeErrProvider) GetSecret(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (closeErrProvider) Reload(context.Context) error               { return nil }
func (closeErrProvider) HealthCheck(context.Context) (bool, string) { return true, "" }
func (closeErrProvider) Close() error                               { return errors.New("close failed") }

func TestSecretNameToConfigKey(t *testing.T) {
	cases := map[string]string{
		"encryption_key": "encryption_key",
		"AUTH_TOKEN":     "auth_token",
		"e2ee_key":       "e2ee_key",
		"e2ee_key_id":    "e2ee_key_id",
		"oprf_share":     "oprf_share",
		"custom_secret":  "custom_secret",
	}
	for in, want := range cases {
		if got := secretNameToConfigKey(in); got != want {
			t.Fatalf("secretNameToConfigKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnvProvider_DefaultPrefixAndClose(t *testing.T) {
	p := NewEnvProvider("")
	if p.prefix != "MOMO_" {
		t.Fatalf("expected default prefix MOMO_, got %q", p.prefix)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestFileProvider_Close(t *testing.T) {
	p := NewFileProvider("does-not-matter.conf")
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestProviderChain_AddAndClose(t *testing.T) {
	chain := NewProviderChain()
	chain.Add(NewEnvProvider("MOMO_"))
	chain.Add(NewFileProvider("conf/momo.conf"))

	if err := chain.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Close must propagate provider errors.
	bad := NewProviderChain(closeErrProvider{})
	if err := bad.Close(); err == nil {
		t.Fatal("expected Close to propagate provider error")
	}
}

func TestNewProviderFromConfig(t *testing.T) {
	if p, err := NewProviderFromConfig(SecretsSourceEnv, SecretsSourceConfig{EnvPrefix: "MOMO_"}); err != nil || p == nil {
		t.Fatalf("env provider: p=%v err=%v", p, err)
	}
	if p, err := NewProviderFromConfig(SecretsSourceFile, SecretsSourceConfig{FilePath: "conf/momo.conf"}); err != nil || p == nil {
		t.Fatalf("file provider: p=%v err=%v", p, err)
	}

	for _, src := range []SecretsSource{SecretsSourceVault, SecretsSourceAWS, SecretsSourceGCP, SecretsSourceAzure, "bogus"} {
		if _, err := NewProviderFromConfig(src, SecretsSourceConfig{}); err == nil {
			t.Fatalf("expected error for source %q", src)
		}
	}
}

func TestBuildProviderChain(t *testing.T) {
	// Disabled -> empty chain, no error.
	disabled, err := BuildProviderChain(SecretsConfig{Enabled: false})
	if err != nil {
		t.Fatalf("BuildProviderChain(disabled): %v", err)
	}
	if len(disabled.providers) != 0 {
		t.Fatalf("disabled chain should be empty, got %d", len(disabled.providers))
	}

	// No explicit sources -> default env + file.
	def, err := BuildProviderChain(SecretsConfig{Enabled: true})
	if err != nil {
		t.Fatalf("BuildProviderChain(default): %v", err)
	}
	if len(def.providers) != 2 {
		t.Fatalf("default chain should have 2 providers, got %d", len(def.providers))
	}

	// Explicit sources with per-source configs.
	explicit, err := BuildProviderChain(SecretsConfig{
		Enabled: true,
		Sources: []SecretsSource{SecretsSourceEnv},
		SourceConfigs: map[SecretsSource]SecretsSourceConfig{
			SecretsSourceEnv: {EnvPrefix: "MOMO_"},
		},
	})
	if err != nil {
		t.Fatalf("BuildProviderChain(explicit): %v", err)
	}
	if len(explicit.providers) != 1 {
		t.Fatalf("explicit chain should have 1 provider, got %d", len(explicit.providers))
	}

	// Unknown source -> error.
	if _, err := BuildProviderChain(SecretsConfig{Enabled: true, Sources: []SecretsSource{"bogus"}}); err == nil {
		t.Fatal("expected error for unknown source")
	}
}
