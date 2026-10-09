package common

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRotationManager_ReloadStoresSecrets(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer db.Close()

	os.Setenv("MOMO_ENCRYPTION_KEY", strings.Repeat("ab", 32)) // 64 hex chars -> decode path
	os.Setenv("MOMO_AUTH_TOKEN", "short-token")                // non-hex path
	defer os.Unsetenv("MOMO_ENCRYPTION_KEY")
	defer os.Unsetenv("MOMO_AUTH_TOKEN")

	chain := NewProviderChain(NewEnvProvider("MOMO_"))
	rm := NewRotationManager(kr, chain, time.Hour)

	if err := rm.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	id, material, err := kr.GetActiveKey(context.Background(), "encryption", "")
	if err != nil {
		t.Fatalf("GetActiveKey: %v", err)
	}
	if id == "" || len(material) == 0 {
		t.Fatal("expected encryption key to be stored by Reload")
	}
}

func TestRotationManager_RotateOnClosedDB(t *testing.T) {
	kr, db := newTestRegistry(t)
	_ = db.Close() // force registry errors

	rm := NewRotationManager(kr, &errProvider{}, time.Hour)
	if err := rm.Rotate(context.Background(), "encryption"); err == nil {
		t.Fatal("expected Rotate to fail on closed DB")
	}
	// RotateAll logs per-purpose failures and returns nil.
	if err := rm.RotateAll(context.Background()); err != nil {
		t.Fatalf("RotateAll: %v", err)
	}
}

func TestKeyRegistry_StoreKeyErrors(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer db.Close()

	if _, err := kr.StoreKey(context.Background(), "encryption", nil, "AES-256-GCM", "", KeyStatusActive, 0); err == nil {
		t.Fatal("expected error for empty material")
	}
	if _, err := kr.StoreKey(context.Background(), "", []byte("x"), "AES-256-GCM", "", KeyStatusActive, 0); err == nil {
		t.Fatal("expected error for empty purpose")
	}
}

func TestFileProvider_NotInitialized(t *testing.T) {
	p := NewFileProvider("conf/momo.conf")
	if _, _, err := p.GetSecret(context.Background(), "encryption_key"); err == nil {
		t.Fatal("expected error when file provider is not initialized")
	}
	healthy, _ := p.HealthCheck(context.Background())
	if healthy {
		t.Fatal("expected uninitialized file provider to be unhealthy")
	}
}

func TestFileProvider_InitErrors(t *testing.T) {
	// Empty path.
	if err := NewFileProvider("").Init(context.Background(), SecretsSourceConfig{}); err == nil {
		t.Fatal("expected error for empty path")
	}
	// Nonexistent file.
	if err := NewFileProvider(filepath.Join(t.TempDir(), "nope.conf")).Init(context.Background(), SecretsSourceConfig{}); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestFileProvider_GetSecretMissAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.conf")
	if err := os.WriteFile(path, []byte("[global]\nencryption_key = abc123\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := NewFileProvider(path)
	if err := p.Init(context.Background(), SecretsSourceConfig{FilePath: path}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if v, found, _ := p.GetSecret(context.Background(), "encryption_key"); !found || v != "abc123" {
		t.Fatalf("expected encryption_key=abc123, got %q found=%v", v, found)
	}
	// Missing key -> not found, no error.
	if _, found, err := p.GetSecret(context.Background(), "missing_key"); err != nil || found {
		t.Fatalf("expected missing key not found, got found=%v err=%v", found, err)
	}

	if err := p.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
}

func TestFileProvider_ReloadErrors(t *testing.T) {
	if err := NewFileProvider("").Reload(context.Background()); err == nil {
		t.Fatal("expected error for empty path reload")
	}
	if err := NewFileProvider(filepath.Join(t.TempDir(), "gone.conf")).Reload(context.Background()); err == nil {
		t.Fatal("expected error for missing file reload")
	}
}
