package common

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"syscall"
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

	var hookSuccess *bool
	rm.RegisterRotationHook(func(purpose, oldKeyID, newKeyID, trigger string, success bool, err error) {
		hookSuccess = &success
	})

	// Reload fires reload hooks.
	if err := rm.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !reloaded {
		t.Fatal("reload hook was not fired")
	}

	// Rotate refuses and reports the refusal to the hook.
	if err := rm.Rotate(context.Background(), "encryption"); !errors.Is(err, ErrRotationNotWired) {
		t.Fatalf("expected ErrRotationNotWired, got %v", err)
	}
	if hookSuccess == nil {
		t.Fatal("rotation hook was not fired")
	}
	if *hookSuccess {
		t.Fatal("a refused rotation must not be reported as a success")
	}
}

func TestRotationManager_ReloadProviderError(t *testing.T) {
	rm := NewRotationManager(nil, &errProvider{reloadErr: errors.New("boom")}, time.Hour)
	if err := rm.Reload(context.Background()); err == nil {
		t.Fatal("expected Reload to fail when provider reload fails")
	}
}

func TestRotationManager_StartIsDisabled(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer db.Close()

	rm := NewRotationManager(kr, &errProvider{}, time.Hour)
	// The scheduler is disabled because rotation is not wired (#1329); Start
	// must be a no-op that is safe to call with any interval, and it must not
	// create a key behind the operator's back.
	rm.Start(context.Background(), 0)
	rm.Start(context.Background(), time.Hour)

	entries, err := readRotationAudit(t, db)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("Start must not rotate, got %d audit entries", len(entries))
	}
}

func TestRotationManager_RotateAll(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer db.Close()

	rm := NewRotationManager(kr, &errProvider{}, time.Hour)
	if err := rm.RotateAll(context.Background()); !errors.Is(err, ErrRotationNotWired) {
		t.Fatalf("expected RotateAll to propagate ErrRotationNotWired, got %v", err)
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

// readRotationAudit decodes every AuditRotationEntry in the audit_log bucket.
func readRotationAudit(t *testing.T, db *bbolt.DB) ([]AuditRotationEntry, error) {
	t.Helper()
	var out []AuditRotationEntry
	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("audit_log"))
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			var e AuditRotationEntry
			if err := json.Unmarshal(v, &e); err != nil {
				return err
			}
			out = append(out, e)
			return nil
		})
	})
	return out, err
}

func TestRotationManager_RotateRefusesEveryPurpose(t *testing.T) {
	kr, db := newTestRegistry(t)
	defer func() { _ = db.Close() }()

	rm := NewRotationManager(kr, &errProvider{}, time.Hour)
	rm.SetAuditDB(db)

	for _, purpose := range []string{"encryption", "auth", "e2ee", "oprf"} {
		if err := rm.Rotate(context.Background(), purpose); !errors.Is(err, ErrRotationNotWired) {
			t.Fatalf("Rotate(%s): expected ErrRotationNotWired, got %v", purpose, err)
		}
		// Nothing may be stored: a key in the registry that no data path reads
		// is exactly the lie this change removes.
		if id, _, err := kr.GetActiveKeyForPurpose(context.Background(), purpose); err == nil && id != "" {
			t.Fatalf("Rotate(%s) stored active key %q", purpose, id)
		}
	}

	entries, err := readRotationAudit(t, db)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected 4 audit entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Success {
			t.Fatalf("refused rotation audited as success: %+v", e)
		}
		if !strings.Contains(e.Error, ErrRotationNotWired.Error()) {
			t.Fatalf("audit entry does not explain the refusal: %q", e.Error)
		}
	}
}

func TestRotationManager_RotateUnknownPurpose(t *testing.T) {
	rm := NewRotationManager(nil, &errProvider{}, time.Hour)
	if err := rm.Rotate(context.Background(), "bogus"); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("expected EINVAL for unknown purpose, got %v", err)
	}
}

func TestAlgorithmForPurpose(t *testing.T) {
	cases := map[string]string{
		"encryption": "AES-256-GCM",
		"e2ee":       "AES-256-GCM",
		"auth":       "raw",
		"oprf":       "raw",
	}
	for purpose, want := range cases {
		if got := algorithmForPurpose(purpose); got != want {
			t.Errorf("algorithmForPurpose(%s) = %q, want %q", purpose, got, want)
		}
	}
}

func TestIsRotationPurpose(t *testing.T) {
	for _, p := range []string{"encryption", "auth", "e2ee", "oprf"} {
		if !isRotationPurpose(p) {
			t.Errorf("%s should be a rotation purpose", p)
		}
	}
	if isRotationPurpose("bogus") {
		t.Error("bogus must not be a rotation purpose")
	}
}
