package common

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// newTestRegistry opens a throwaway BoltDB and wraps it in a KeyRegistry.
func newTestRegistry(t *testing.T) (*KeyRegistry, *bbolt.DB) {
	t.Helper()
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "rot.db"), 0600, nil)
	if err != nil {
		t.Fatalf("open bbolt: %v", err)
	}
	kr, err := NewKeyRegistry(db)
	if err != nil {
		db.Close()
		t.Fatalf("NewKeyRegistry: %v", err)
	}
	return kr, db
}

type errProvider struct{ reloadErr error }

func (e *errProvider) Init(context.Context, SecretsSourceConfig) error { return nil }
func (e *errProvider) GetSecret(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (e *errProvider) Reload(context.Context) error               { return e.reloadErr }
func (e *errProvider) HealthCheck(context.Context) (bool, string) { return true, "" }
func (e *errProvider) Close() error                               { return nil }

func TestNewRotationManager_DefaultGrace(t *testing.T) {
	rm := NewRotationManager(nil, &errProvider{}, 0)
	if rm.gracePeriod != 24*time.Hour {
		t.Fatalf("expected default 24h grace, got %v", rm.gracePeriod)
	}
	rm2 := NewRotationManager(nil, &errProvider{}, time.Hour)
	if rm2.gracePeriod != time.Hour {
		t.Fatalf("expected 1h grace, got %v", rm2.gracePeriod)
	}
}

func TestRotationManager_Hooks(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer db.Close()

	rm := NewRotationManager(kr, &errProvider{}, time.Hour)

	reloaded := false
	rm.RegisterReloadHook(func() { reloaded = true })

	rotated := false
	rm.RegisterRotationHook(func(purpose, oldKeyID, newKeyID, trigger string, success bool, err error) {
		rotated = true
	})

	// Reload fires reload hooks.
	if err := rm.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !reloaded {
		t.Fatal("reload hook was not fired")
	}

	// Rotate fires rotation hooks.
	if err := rm.Rotate(context.Background(), "encryption"); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if !rotated {
		t.Fatal("rotation hook was not fired")
	}
}

func TestRotationManager_ReloadProviderError(t *testing.T) {
	rm := NewRotationManager(nil, &errProvider{reloadErr: errors.New("boom")}, time.Hour)
	if err := rm.Reload(context.Background()); err == nil {
		t.Fatal("expected Reload to fail when provider reload fails")
	}
}

func TestRotationManager_StartStop(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer db.Close()

	// interval <= 0 disables scheduling and does not set a ticker.
	rmDisabled := NewRotationManager(kr, &errProvider{}, time.Hour)
	rmDisabled.Start(context.Background(), 0)
	if rmDisabled.ticker != nil {
		t.Fatal("ticker should not be set for non-positive interval")
	}

	// interval > 0 starts the scheduler; Stop must clean it up.
	rm := NewRotationManager(kr, &errProvider{}, time.Hour)
	rm.Start(context.Background(), time.Hour)
	if rm.ticker == nil {
		t.Fatal("ticker should be set for positive interval")
	}
	rm.Stop()
}

func TestRotationManager_StartContextCancel(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	rm := NewRotationManager(kr, &errProvider{}, time.Hour)
	rm.Start(ctx, time.Hour)
	cancel() // goroutine exits via ctx.Done()
}

func TestRotationManager_RotateAll(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer db.Close()

	rm := NewRotationManager(kr, &errProvider{}, time.Hour)
	if err := rm.RotateAll(context.Background()); err != nil {
		t.Fatalf("RotateAll: %v", err)
	}
}

func TestRotationManager_GenerateKeyForPurpose(t *testing.T) {
	rm := NewRotationManager(nil, &errProvider{}, time.Hour)

	for _, purpose := range []string{"encryption", "auth", "e2ee"} {
		key, err := rm.generateKeyForPurpose(purpose)
		if err != nil {
			t.Fatalf("generateKeyForPurpose(%s): %v", purpose, err)
		}
		if len(key) != 32 {
			t.Fatalf("expected 32-byte key for %s, got %d", purpose, len(key))
		}
	}

	if _, err := rm.generateKeyForPurpose("oprf"); err != nil {
		t.Fatalf("generateKeyForPurpose(oprf): %v", err)
	}

	if _, err := rm.generateKeyForPurpose("bogus"); err == nil {
		t.Fatal("expected error for unknown purpose")
	}
}

func TestRotationManager_ReloadHookChannel(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer db.Close()

	rm := NewRotationManager(kr, &errProvider{}, time.Hour)
	ch := rm.ReloadHook()
	if err := rm.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("reload hook channel did not fire")
	}
}

func TestRotationManager_Status(t *testing.T) {
	rm := NewRotationManager(nil, &errProvider{}, time.Hour)
	status := rm.Status()
	if status.ActiveKeys == nil {
		t.Fatal("Status.ActiveKeys should be non-nil")
	}
}
