package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alsotoes/momo/src/common"
)

func TestInitSecretsManager_Disabled(t *testing.T) {
	rm, db := initSecretsManager(context.Background(), common.Configuration{}, t.TempDir())
	if rm != nil || db != nil {
		t.Fatalf("expected (nil, nil) when disabled, got rm=%v db=%v", rm, db)
	}
}

func TestInitSecretsManager_Enabled(t *testing.T) {
	cfg := common.Configuration{
		Secrets: common.SecretsConfig{
			Enabled: true,
			Sources: []common.SecretsSource{common.SecretsSourceEnv},
		},
	}
	rm, db := initSecretsManager(context.Background(), cfg, t.TempDir())
	if rm == nil {
		t.Fatal("expected a rotation manager")
	}
	if db == nil {
		t.Fatal("expected a key registry DB handle")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close key DB: %v", err)
	}
}

func TestInitSecretsManager_BadProvider(t *testing.T) {
	cfg := common.Configuration{
		Secrets: common.SecretsConfig{
			Enabled: true,
			Sources: []common.SecretsSource{"bogus"},
		},
	}
	rm, db := initSecretsManager(context.Background(), cfg, t.TempDir())
	if rm != nil {
		t.Fatal("expected nil rotation manager on provider error")
	}
	if db == nil {
		t.Fatal("expected the opened DB handle to be returned for cleanup")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close key DB: %v", err)
	}
}

func TestInitSecretsManager_UnopenableDB(t *testing.T) {
	// Point dataDir at a regular file so bbolt.Open fails.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	cfg := common.Configuration{
		Secrets: common.SecretsConfig{
			Enabled: true,
			Sources: []common.SecretsSource{common.SecretsSourceEnv},
		},
	}
	rm, db := initSecretsManager(context.Background(), cfg, filePath)
	if rm != nil || db != nil {
		t.Fatalf("expected (nil, nil) on unopenable DB, got rm=%v db=%v", rm, db)
	}
}
