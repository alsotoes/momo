// Package common provides shared functionality for the momo application.
package common

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"syscall"
	"time"
)

// rotationPurposes is the fixed set of key purposes rotated by the manager.
var rotationPurposes = []string{"encryption", "auth", "e2ee", "oprf"}

// RotationManager manages automated and manual key rotation.
// It handles scheduled rotation, grace periods, and hot reload signaling.
type RotationManager struct {
	keyRegistry   *KeyRegistry
	provider      SecretProvider
	gracePeriod   time.Duration
	ticker        *time.Ticker
	stopCh        chan struct{}
	mu            sync.RWMutex
	reloadHooks   []func()
	rotationHooks []func(purpose string, oldKeyID, newKeyID string, trigger string, success bool, err error)
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
		stopCh:        make(chan struct{}),
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

// Start begins the scheduled rotation ticker.
// It runs until Stop() is called or context is cancelled.
func (rm *RotationManager) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		log.Println("Rotation interval not set or invalid, scheduled rotation disabled")
		return
	}

	rm.ticker = time.NewTicker(interval)
	log.Printf("Key rotation scheduler started with interval %v", interval)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("CRITICAL: Panic recovered in rotation scheduler: %v", r)
			}
		}()
		for {
			select {
			case <-rm.ticker.C:
				// Run rotation for all purposes
				if err := rm.RotateAll(context.Background()); err != nil {
					log.Printf("Scheduled rotation failed: %v", err)
				}
			case <-rm.stopCh:
				rm.ticker.Stop()
				return
			case <-ctx.Done():
				rm.ticker.Stop()
				return
			}
		}
	}()
}

// Stop stops the rotation scheduler.
func (rm *RotationManager) Stop() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.ticker != nil {
		rm.ticker.Stop()
	}
	close(rm.stopCh)
}

// Reload triggers a hot reload of secrets from the provider.
// It fetches fresh secrets and updates the key registry.
func (rm *RotationManager) Reload(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in Reload: %v", r)
			err = fmt.Errorf("reload panic: %w", syscall.EIO)
		}
	}()
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

		val, found, err := rm.provider.GetSecret(context.Background(), secretName)
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
		keyID, err := rm.keyRegistry.StoreKey(context.Background(), purpose, material, "AES-256-GCM", "", KeyStatusActive, 0)
		if err != nil {
			log.Printf("Failed to store key for %s: %v", purpose, err)
			continue
		}
		log.Printf("Updated key for %s: %s", purpose, keyID)
	}

	rm.fireReloadHooks()
	return nil
}

// Rotate triggers a manual rotation for a specific purpose.
// It generates a new key, stores it as the active key (retiring the previous
// one, which remains readable for decryption), and signals a hot reload.
func (rm *RotationManager) Rotate(ctx context.Context, purpose string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in Rotate: %v", r)
			err = fmt.Errorf("rotate panic: %w", syscall.EIO)
		}
	}()
	log.Printf("Manual rotation triggered for %s", purpose)

	// Capture the current active key so the old/new IDs can be audited.
	oldKeyID, _, err := rm.keyRegistry.GetActiveKeyForPurpose(ctx, purpose)
	if err != nil {
		rm.fireRotationHooks(purpose, "", "", "manual", false, err)
		return fmt.Errorf("failed to get active key: %w", err)
	}

	// Generate new key material.
	material, err := rm.generateKeyForPurpose(purpose)
	if err != nil {
		rm.fireRotationHooks(purpose, oldKeyID, "", "manual", false, err)
		return fmt.Errorf("failed to generate new key: %w", err)
	}

	// Persist the new key as pending, then activate it (demotes the old one).
	newKeyID, err := rm.keyRegistry.StoreKey(ctx, purpose, material, "AES-256-GCM", "", KeyStatusPendingRotation, 0)
	if err != nil {
		rm.fireRotationHooks(purpose, oldKeyID, "", "manual", false, err)
		return fmt.Errorf("failed to store new key: %w", err)
	}
	if err := rm.keyRegistry.SetActive(ctx, newKeyID); err != nil {
		rm.fireRotationHooks(purpose, oldKeyID, newKeyID, "manual", false, err)
		return fmt.Errorf("failed to activate new key: %w", err)
	}

	// Signal dependent components to re-read keys from the registry.
	rm.fireReloadHooks()

	rm.fireRotationHooks(purpose, oldKeyID, newKeyID, "manual", true, nil)
	log.Printf("Rotation completed for %s (grace period %v)", purpose, rm.gracePeriod)
	return nil
}

// RotateAll rotates all known purposes.
func (rm *RotationManager) RotateAll(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in RotateAll: %v", r)
			err = fmt.Errorf("rotate-all panic: %w", syscall.EIO)
		}
	}()
	for _, purpose := range rotationPurposes {
		if err := rm.Rotate(ctx, purpose); err != nil {
			log.Printf("Rotation failed for %s: %v", purpose, err)
		}
	}
	return nil
}

// generateKeyForPurpose generates a new key for the given purpose.
func (rm *RotationManager) generateKeyForPurpose(purpose string) ([]byte, error) {
	switch purpose {
	case "encryption", "auth", "e2ee":
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		return key, nil
	case "oprf":
		// For OPRF, we'd generate Shamir shares
		// This is a placeholder
		return []byte("oprf-share-placeholder"), nil
	default:
		return nil, fmt.Errorf("unknown purpose: %s", purpose)
	}
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
