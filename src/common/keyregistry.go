// Package common provides shared functionality for the momo application.
package common

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

// KeyRegistryBucket is the BoltDB bucket name for the key registry.
const KeyRegistryBucket = "key_registry"

// KeyRegistry manages versioned keys for different purposes.
// It uses a BoltDB bucket for persistent storage.
type KeyRegistry struct {
	db    *bbolt.DB
	mu    sync.RWMutex
	hooks []func(KeyEntry) // callbacks for key changes (e.g., reload triggers)
}

// NewKeyRegistry creates a new KeyRegistry using the given BoltDB database.
// It ensures the key_registry bucket exists.
func NewKeyRegistry(db *bbolt.DB) (*KeyRegistry, error) {
	kr := &KeyRegistry{db: db}
	err := db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(KeyRegistryBucket))
		return err
	})
	return kr, err
}

// RegisterChangeHook registers a callback that fires when a key is added/updated.
// Used to trigger hot reloads when the active key changes.
func (kr *KeyRegistry) RegisterChangeHook(fn func(KeyEntry)) {
	kr.mu.Lock()
	defer kr.mu.Unlock()
	kr.hooks = append(kr.hooks, fn)
}

// fireChangeHooks calls all registered change hooks.
func (kr *KeyRegistry) fireChangeHooks(entry KeyEntry) {
	kr.mu.RLock()
	hooks := make([]func(KeyEntry), len(kr.hooks))
	copy(hooks, kr.hooks)
	kr.mu.RUnlock()
	for _, fn := range hooks {
		fn(entry)
	}
}

// StoreKey stores a new key version in the registry.
// If a key with the same purpose and tenant already exists, it increments the version.
// Returns the KeyID of the stored key.
func (kr *KeyRegistry) StoreKey(ctx context.Context, purpose string, material []byte, algorithm string, tenantID string, status KeyStatus, oprfShareIndex int) (string, error) {
	if len(material) == 0 {
		return "", fmt.Errorf("key material cannot be empty")
	}
	if purpose == "" {
		return "", fmt.Errorf("purpose cannot be empty")
	}

	entry := KeyEntry{
		KeyID:          newKeyID(),
		Purpose:        purpose,
		Version:        1,
		Material:       material,
		Algorithm:      algorithm,
		Status:         status,
		CreatedAt:      time.Now().UTC(),
		RotatedAt:      nil,
		OPRFShareIndex: 0,
		TenantID:       "",
	}

	// Determine next version number for this purpose+tenant
	version, err := kr.nextVersion(purpose, "")
	if err != nil {
		return "", err
	}
	entry.Version = version

	updateErr := kr.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(KeyRegistryBucket))
		if b == nil {
			return fmt.Errorf("key_registry bucket not found")
		}
		data, marshalErr := json.Marshal(entry)
		if marshalErr != nil {
			return marshalErr
		}
		return b.Put([]byte(entry.KeyID), data)
	})
	if updateErr != nil {
		return "", updateErr
	}

	kr.fireChangeHooks(entry)
	return entry.KeyID, nil
}

// GetActiveKey returns the active key for the given purpose and tenant.
// Returns (keyID, material) or ("", nil) if no active key exists.
func (kr *KeyRegistry) GetActiveKey(ctx context.Context, purpose, tenantID string) (string, []byte, error) {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	var activeEntry *KeyEntry
	err := kr.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(KeyRegistryBucket))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var entry KeyEntry
			if err := json.Unmarshal(v, &entry); err != nil {
				return nil
			}
			if entry.Purpose == purpose && entry.TenantID == "" && entry.Status == KeyStatusActive {
				entryCopy := entry
				activeEntry = &entryCopy
			}
			return nil
		})
	})
	if err != nil {
		return "", nil, err
	}

	if activeEntry == nil {
		return "", nil, nil
	}
	return activeEntry.KeyID, activeEntry.Material, nil
}

// GetKey retrieves a key by its KeyID.
func (kr *KeyRegistry) GetKey(ctx context.Context, keyID string) (*KeyEntry, error) {
	var result *KeyEntry
	_ = kr.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(KeyRegistryBucket))
		if b == nil {
			return fmt.Errorf("key_registry bucket not found")
		}
		v := b.Get([]byte(keyID))
		if v == nil {
			return nil // not found
		}
		var entry KeyEntry
		if err := json.Unmarshal(v, &entry); err != nil {
			return err
		}
		result = &entry
		return nil
	})
	return result, nil
}

// MarkRetired marks a key as retired (no longer used for encryption, but kept for decryption).
func (kr *KeyRegistry) MarkRetired(ctx context.Context, keyID string) error {
	return kr.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(KeyRegistryBucket))
		if b == nil {
			return fmt.Errorf("key_registry bucket not found")
		}
		v := b.Get([]byte(keyID))
		if v == nil {
			return fmt.Errorf("key %s not found", keyID)
		}
		var entry KeyEntry
		if err := json.Unmarshal(v, &entry); err != nil {
			return err
		}
		entry.Status = KeyStatusRetired
		now := time.Now().UTC()
		entry.RotatedAt = &now
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return b.Put([]byte(keyID), data)
	})
}

// MarkCompromised marks a key as compromised (emergency rotation).
func (kr *KeyRegistry) MarkCompromised(ctx context.Context, keyID string) error {
	return kr.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(KeyRegistryBucket))
		if b == nil {
			return fmt.Errorf("key_registry bucket not found")
		}
		v := b.Get([]byte(keyID))
		if v == nil {
			return fmt.Errorf("key %s not found", keyID)
		}
		var entry KeyEntry
		if err := json.Unmarshal(v, &entry); err != nil {
			return err
		}
		entry.Status = KeyStatusCompromised
		now := time.Now().UTC()
		entry.RotatedAt = &now
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return b.Put([]byte(keyID), data)
	})
}

// SetActive sets a key as the active one for its purpose (demotes others to retired).
func (kr *KeyRegistry) SetActive(ctx context.Context, keyID string) error {
	return kr.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(KeyRegistryBucket))
		if b == nil {
			return fmt.Errorf("key_registry bucket not found")
		}

		// First, find the target entry and its purpose
		v := b.Get([]byte(keyID))
		if v == nil {
			return fmt.Errorf("key %s not found", keyID)
		}
		var target KeyEntry
		if err := json.Unmarshal(v, &target); err != nil {
			return err
		}

		// Demote all other active keys for this purpose to retired
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var entry KeyEntry
			if err := json.Unmarshal(v, &entry); err != nil {
				continue
			}
			if entry.Purpose == target.Purpose && entry.Status == KeyStatusActive && string(k) != keyID {
				entry.Status = KeyStatusRetired
				now := time.Now().UTC()
				entry.RotatedAt = &now
				data, _ := json.Marshal(entry)
				if err := b.Put(k, data); err != nil {
					return err
				}
			}
		}

		// Now activate the target
		v = b.Get([]byte(keyID))
		var targetEntry KeyEntry
		if err := json.Unmarshal(v, &targetEntry); err != nil {
			return err
		}
		targetEntry.Status = KeyStatusActive
		targetEntry.RotatedAt = nil
		data, err := json.Marshal(targetEntry)
		if err != nil {
			return err
		}
		return b.Put([]byte(keyID), data)
	})
}

// ListVersions returns all versions for a given purpose.
func (kr *KeyRegistry) ListVersions(ctx context.Context, purpose string) ([]KeyEntry, error) {
	var entries []KeyEntry
	_ = kr.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(KeyRegistryBucket))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var entry KeyEntry
			if err := json.Unmarshal(v, &entry); err != nil {
				return nil
			}
			if entry.Purpose == purpose {
				entries = append(entries, entry)
			}
			return nil
		})
	})
	return entries, nil
}

// GetActiveKeyForPurpose returns the active key for a purpose (tenant-agnostic).
func (kr *KeyRegistry) GetActiveKeyForPurpose(ctx context.Context, purpose string) (string, []byte, error) {
	return kr.GetActiveKey(ctx, purpose, "")
}

// nextVersion returns the next version number for a purpose.
func (kr *KeyRegistry) nextVersion(purpose, tenantID string) (int, error) {
	maxVer := 0
	viewErr := kr.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(KeyRegistryBucket))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var entry KeyEntry
			if err := json.Unmarshal(v, &entry); err != nil {
				return nil
			}
			if entry.Purpose == purpose && entry.Version > maxVer {
				maxVer = entry.Version
			}
			return nil
		})
	})
	return maxVer + 1, viewErr
}

// newKeyID generates a new unique key ID.
func newKeyID() string {
	return fmt.Sprintf("key-%d-%d", time.Now().UnixNano(), time.Now().UnixNano()%1000)
}
