package common

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSecretProviderChain(t *testing.T) {
	tmpDir := t.TempDir()

	// Test 1: EnvProvider
	os.Setenv("MOMO_TEST_KEY", "test-value")
	defer os.Unsetenv("MOMO_TEST_KEY")

	envProvider := NewEnvProvider("MOMO_")
	ctx := context.Background()
	if err := envProvider.Init(ctx, SecretsSourceConfig{EnvPrefix: "MOMO_"}); err != nil {
		t.Fatalf("EnvProvider.Init failed: %v", err)
	}

	val1, found1, err := envProvider.GetSecret(ctx, "test_key")
	if err != nil {
		t.Fatalf("EnvProvider.GetSecret error: %v", err)
	}
	if !found1 {
		t.Fatal("EnvProvider.GetSecret should find MOMO_TEST_KEY")
	}
	if val1 != "test-value" {
		t.Fatalf("EnvProvider.GetSecret returned %q, want %q", val1, "test-value")
	}

	// Test 2: FileProvider
	iniPath := filepath.Join(tmpDir, "test.conf")
	if err := os.WriteFile(filepath.Join(tmpDir, "test.conf"), []byte(`[global]
test_key = file-value
`), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	fileProvider := NewFileProvider(iniPath)
	if err := fileProvider.Init(context.Background(), SecretsSourceConfig{FilePath: iniPath}); err != nil {
		t.Fatalf("FileProvider.Init failed: %v", err)
	}

	valF1, foundF1, err := fileProvider.GetSecret(ctx, "test_key")
	if err != nil {
		t.Fatalf("FileProvider.GetSecret error: %v", err)
	}
	if !foundF1 {
		t.Fatal("FileProvider.GetSecret should find test_key")
	}
	if valF1 != "file-value" {
		t.Fatalf("FileProvider.GetSecret returned %q, want %q", valF1, "file-value")
	}

	// Test 3: ProviderChain - env first, then file
	chain := NewProviderChain(
		NewEnvProvider("MOMO_"),
		NewFileProvider(iniPath),
	)
	if err := chain.Init(ctx, SecretsSourceConfig{}); err != nil {
		t.Fatalf("ProviderChain.Init failed: %v", err)
	}

	// Env should take precedence
	valC1, foundC1, err := chain.GetSecret(ctx, "test_key")
	if err != nil {
		t.Fatalf("chain.GetSecret error: %v", err)
	}
	if !foundC1 {
		t.Fatal("chain.GetSecret should find test_key")
	}
	if valC1 != "test-value" {
		t.Fatalf("chain.GetSecret returned %q, want %q (env should win)", valC1, "test-value")
	}

	// Test 4: ProviderChain - file only (env not set)
	os.Unsetenv("MOMO_TEST_KEY")
	defer os.Setenv("MOMO_TEST_KEY", "test-value")

	chain2 := NewProviderChain(
		NewEnvProvider("MOMO_"),
		NewFileProvider(iniPath),
	)
	if err := chain2.Init(ctx, SecretsSourceConfig{}); err != nil {
		t.Fatalf("chain2.Init failed: %v", err)
	}

	valC2, foundC2, err := chain2.GetSecret(ctx, "test_key")
	if err != nil {
		t.Fatalf("chain2.GetSecret error: %v", err)
	}
	if !foundC2 {
		t.Fatal("chain2.GetSecret should find test_key in file")
	}
	if valC2 != "file-value" {
		t.Fatalf("chain2.GetSecret returned %q, want %q", valC2, "file-value")
	}

	// Test 5: Reload
	if err := chain.Reload(ctx); err != nil {
		t.Fatalf("chain.Reload failed: %v", err)
	}

	// Test 6: HealthCheck
	healthy, details := chain.HealthCheck(ctx)
	if !healthy {
		t.Fatalf("chain.HealthCheck failed: %s", details)
	}
}
