package common

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCoordinationModeStoreRoundTrip(t *testing.T) {
	store := NewCoordinationModeStore(t.TempDir())
	if store == nil {
		t.Fatal("expected a store")
	}
	if _, ok, err := store.LoadMode(); err != nil || ok {
		t.Fatalf("empty store: ok=%v err=%v", ok, err)
	}
	if err := store.SaveMode(3); err != nil {
		t.Fatalf("SaveMode: %v", err)
	}
	mode, ok, err := store.LoadMode()
	if err != nil || !ok || mode != 3 {
		t.Fatalf("LoadMode = %d,%v,%v", mode, ok, err)
	}
	// Overwrite wins.
	if err := store.SaveMode(1); err != nil {
		t.Fatalf("SaveMode overwrite: %v", err)
	}
	if mode, _, _ := store.LoadMode(); mode != 1 {
		t.Fatalf("expected overwritten mode 1, got %d", mode)
	}
}

func TestCoordinationModeStoreRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	store := NewCoordinationModeStore(dir)
	if err := os.MkdirAll(filepath.Join(dir, "coordination"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte("not-a-number"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.LoadMode(); err == nil {
		t.Fatal("garbage must be reported as an error")
	}
}

func TestCoordinationModeStoreDisabled(t *testing.T) {
	var store *CoordinationModeStore
	if mode, ok, err := store.LoadMode(); mode != 0 || ok || err != nil {
		t.Fatalf("nil store must be a no-op, got %d,%v,%v", mode, ok, err)
	}
	if err := store.SaveMode(2); err != nil {
		t.Fatalf("nil SaveMode: %v", err)
	}
	if store.Path() != "" {
		t.Fatalf("nil store has no path, got %q", store.Path())
	}
	if NewCoordinationModeStore("  ") != nil {
		t.Fatal("blank data dir must yield a nil store")
	}
}
