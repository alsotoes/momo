// Package common provides shared functionality for the momo application.
package common

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sync"
	"syscall"
	"time"

	"go.etcd.io/bbolt"
)

// rotationPurposes is the fixed set of key purposes the manager knows about.
var rotationPurposes = []string{"encryption", "auth", "e2ee", "oprf"}

// ErrRotationNotWired reports that generating a new key would change nothing:
// the running daemon reads its secrets from the configuration (or the provider
// chain) at startup, and no data path consults the key registry. Rotation is
// therefore refused instead of audited as a success, because a key an operator
// believes is live but is not is worse than no rotation at all (issue #1329).
var ErrRotationNotWired = errors.New("key rotation is not wired to the data path")

// rotationNotWiredReason is the operator-facing explanation, kept in one place
// so the CLI, the scheduler and the API all say the same thing.
const rotationNotWiredReason = "the daemon keeps using its configured secret and no data path reads the key registry; " +
	"provider secrets can be reloaded with SIGHUP or POST /reload-secrets; see https://github.com/alsotoes/momo/issues/1329"

// isRotationPurpose reports whether purpose is one the manager knows how to
// describe, independent of whether rotation is wired.
func isRotationPurpose(purpose string) bool {
	for _, p := range rotationPurposes {
		if p == purpose {
			return true
		}
	}
	return false
}

// algorithmForPurpose names the key material honestly: only encryption and
// end-to-end-encryption keys are AES-256-GCM keys. The auth token and the OPRF
// share are raw byte strings.
func algorithmForPurpose(purpose string) string {
	switch purpose {
	case "encryption", "e2ee":
		return "AES-256-GCM"
	default:
		return "raw"
	}
}

// RotationManager manages automated and manual key rotation.
// It handles scheduled rotation, grace periods, and hot reload signaling.
type RotationManager struct {
	keyRegistry   *KeyRegistry
	provider      SecretProvider
	gracePeriod   time.Duration
	mu            sync.RWMutex
	reloadHooks   []func()
	rotationHooks []func(purpose string, oldKeyID, newKeyID string, trigger string, success bool, err error)
	auditDB       *bbolt.DB
}

// NewRotationManager creates a new RotationManager.
func NewRotationManager(keyRegistry *KeyRegistry, provider SecretProvider, gracePeriod time.Duration) *RotationManager {
	if gracePeriod <= 0 {
		gracePeriod = 24 * time.Hour // default 24h
	}
	return &RotationManager{
		keyRegistry:   keyRegistry,
		provider:      provider,
		gracePeriod:   gracePeriod,
		reloadHooks:   make([]func(), 0),
		rotationHooks: make([]func(string, string, string, string, bool, error), 0),
	}
}

// RegisterReloadHook registers a callback that fires when keys are reloaded.
// Used to trigger hot reload of keys in dependent components (e.g., storage, server).
func (rm *RotationManager) RegisterReloadHook(fn func()) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.reloadHooks = append(rm.reloadHooks, fn)
}

// RegisterRotationHook registers a callback that fires on rotation completion.
func (rm *RotationManager) RegisterRotationHook(fn func(purpose, oldKeyID, newKeyID, trigger string, success bool, err error)) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.rotationHooks = append(rm.rotationHooks, fn)
}

// SetAuditDB attaches a BoltDB used to record rotation events (R8/R9). When
// nil, rotations are not audited.
func (rm *RotationManager) SetAuditDB(db *bbolt.DB) {
	rm.mu.Lock()
	rm.auditDB = db
	rm.mu.Unlock()
}

// auditRotation records a rotation outcome when an audit DB is configured.
func (rm *RotationManager) auditRotation(ctx context.Context, entry AuditRotationEntry) {
	rm.mu.RLock()
	db := rm.auditDB
	rm.mu.RUnlock()
	if db == nil {
		return
	}
	if err := AuditRotation(ctx, db, entry); err != nil {
		log.Printf("AUDIT: failed to record rotation audit: %v", err)
	}
}

// fireReloadHooks calls all registered reload hooks.
func (rm *RotationManager) fireReloadHooks() {
	rm.mu.RLock()
	hooks := make([]func(), len(rm.reloadHooks))
	copy(hooks, rm.reloadHooks)
	rm.mu.RUnlock()
	for _, fn := range hooks {
		fn()
	}
}

// fireRotationHooks calls all registered rotation hooks.
func (rm *RotationManager) fireRotationHooks(purpose, oldKeyID, newKeyID, trigger string, success bool, err error) {
	rm.mu.RLock()
	hooks := make([]func(string, string, string, string, bool, error), len(rm.rotationHooks))
	copy(hooks, rm.rotationHooks)
	rm.mu.RUnlock()
	for _, fn := range hooks {
		fn(purpose, oldKeyID, newKeyID, trigger, success, err)
	}
}

// Start reports that scheduled rotation is disabled. The ticker, the stop
// channel and the goroutine that used to drive them are gone: rotation is not
// wired to the data path (#1329), so a timer could only produce a failure every
// interval. The signature is kept so the daemon wiring does not need to change.
func (rm *RotationManager) Start(_ context.Context, interval time.Duration) {
	if interval <= 0 {
		log.Println("Rotation interval not set or invalid, scheduled rotation disabled")
		return
	}
	log.Printf("R9: scheduled key rotation disabled with interval %v: %v: %s", interval, ErrRotationNotWired, rotationNotWiredReason)
}

// Reload triggers a hot reload of secrets from the provider.
// It fetches fresh secrets and updates the key registry.
func (rm *RotationManager) Reload(ctx context.Context) (err error) {
	defer RecoverErr("Reload", &err)
	log.Println("Reloading secrets from provider...")
	if err := rm.provider.Reload(ctx); err != nil {
		return fmt.Errorf("provider reload failed: %w", err)
	}

	// Fetch fresh secrets for all known purposes
	purposes := rotationPurposes
	for _, purpose := range purposes {
		var secretName string
		switch purpose {
		case "auth":
			secretName = "auth_token"
		case "e2ee":
			secretName = "e2ee_key"
		case "oprf":
			secretName = "oprf_share"
		default:
			secretName = purpose + "_key"
		}

		val, found, err := rm.provider.GetSecret(ctx, secretName)
		if err != nil {
			log.Printf("Failed to fetch secret %s: %v", secretName, err)
			continue
		}
		if !found {
			continue
		}

		// Decode hex if needed
		material := []byte(val)
		if len(val) == 64 { // hex encoded
			decoded, err := hex.DecodeString(val)
			if err != nil {
				log.Printf("Failed to decode %s: %v", secretName, err)
				continue
			}
			material = decoded
		}

		// Store in registry (will create new version if changed)
		keyID, err := rm.keyRegistry.StoreKey(ctx, purpose, material, algorithmForPurpose(purpose), "", KeyStatusActive, 0)
		if err != nil {
			log.Printf("Failed to store key for %s: %v", purpose, err)
			continue
		}
		log.Printf("Updated key for %s: %s", purpose, keyID)
	}

	rm.fireReloadHooks()
	log.Printf("R9: provider secrets reloaded into the key registry; the running daemon keeps using its configured secrets (%v)", ErrRotationNotWired)
	return nil
}

// Rotate refuses: see ErrRotationNotWired. It no longer generates or stores a
// key, because nothing consumes the registry, and it never reports success.
func (rm *RotationManager) Rotate(ctx context.Context, purpose string) (err error) {
	defer RecoverErr("Rotate", &err)
	if !isRotationPurpose(purpose) {
		err = fmt.Errorf("unknown rotation purpose %q: %w", purpose, syscall.EINVAL)
	} else {
		err = fmt.Errorf("%w: %s: %w", ErrRotationNotWired, rotationNotWiredReason, syscall.ENOTSUP)
	}
	log.Printf("AUDIT: rotation refused for %s: %v", purpose, err)
	rm.auditRotation(ctx, AuditRotationEntry{Purpose: purpose, Operator: "system", Trigger: "manual", Success: false, Error: err.Error()})
	rm.fireRotationHooks(purpose, "", "", "manual", false, err)
	return err
}

// RotateAll rotates all known purposes. Every purpose currently refuses with
// ErrRotationNotWired; the first failure is returned so callers cannot mistake
// a no-op for a completed rotation.
func (rm *RotationManager) RotateAll(ctx context.Context) (err error) {
	defer RecoverErr("RotateAll", &err)
	var first error
	for _, purpose := range rotationPurposes {
		if rerr := rm.Rotate(ctx, purpose); rerr != nil {
			if first == nil {
				first = rerr
			}
		}
	}
	return first
}

// ReloadHook returns a channel that fires when reload is triggered.
// Useful for components that need to react to key changes.
func (rm *RotationManager) ReloadHook() chan struct{} {
	ch := make(chan struct{}, 1)
	rm.RegisterReloadHook(func() {
		select {
		case ch <- struct{}{}:
		default:
		}
	})
	return ch
}

// RotationStatus returns the current rotation status for monitoring.
type RotationStatus struct {
	LastRotation time.Time
	NextRotation time.Time
	ActiveKeys   map[string]string // purpose -> keyID
}

func (rm *RotationManager) Status() RotationStatus {
	return RotationStatus{
		ActiveKeys: map[string]string{},
	}
}
