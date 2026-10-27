package storage

// Object Lock implements the R10-LK1 scenarios (issue #938). Decision record
// (agreed with alsotoes, 2026-10-27):
//   - Overwrite of a locked key is ALLOWED (creates a new version; the locked
//     version stays intact — WORM preserved). Only deletion is denied.
//   - GOVERNANCE retention yields to the S3 governance-bypass flag;
//     COMPLIANCE retention and legal holds never do.
//   - Boundary: lock state is S3-gateway-scoped. The native protocol's Delete
//     tombstones the name but does not consume lock records; blob bytes behind
//     a locked version stay protected because the version record persists and
//     the GC exclusion (versioning.go) keeps the blob referenced.
// Retention/legal-hold deadline comparisons take the clock from the caller.

import (
	"encoding/json"
	"fmt"
	"syscall"
	"time"

	bolt "go.etcd.io/bbolt"
	"github.com/alsotoes/momo/src/common"
)

const (
	// bucketObjectLock holds one flag record per lock-enabled bucket.
	bucketObjectLock = "object_lock"

	// bucketLockStatus holds per-version lock records (JSON ObjectLockState)
	// keyed bucket\x00key\x00versionID.
	bucketLockStatus = "object_lock_state"
)

// S3-compatible object-lock modes.
const (
	LockModeCompliance = "COMPLIANCE"
	LockModeGovernance = "GOVERNANCE"
)

// ObjectLockState is the per-version lock record.
type ObjectLockState struct {
	// Mode is COMPLIANCE or GOVERNANCE; empty means hold-only (no retention).
	Mode string `json:"mode,omitempty"`

	// RetainUntil is the retention deadline in unix UTC seconds; 0 = none.
	RetainUntil int64 `json:"retain_until,omitempty"`

	// LegalHoldOn pins the version regardless of retention.
	LegalHoldOn bool `json:"legal_hold_on,omitempty"`
}

// lockedAt reports whether the record denies deletion at unix-second time now.
func (l ObjectLockState) lockedAt(now int64) bool {
	if l.LegalHoldOn {
		return true
	}
	// Retention without a mode is malformed — treat as unlocked rather than
	// inventing an implicit COMPLIANCE that would wedge data forever.
	if l.Mode == "" {
		return false
	}
	return l.RetainUntil > now
}

func lockStatusKey(bucket, key, versionID string) []byte {
	return []byte(bucket + "\x00" + key + "\x00" + versionID)
}

// SetBucketObjectLock enables object lock for a bucket. There is no disable
// path: a momo lock-enabled bucket can never be unlocked (R10-LK1 scenario 1).
// Lock implies versioning-enabled (S3 semantics); enabling on a
// suspended-versioning bucket is refused so WORM can silently stop capturing
// versions.
func (s *CASStore) SetBucketObjectLock(bucket string) (err error) {
	defer common.RecoverErr("CASStore.SetBucketObjectLock", &err)
	if bucket == "" {
		return syscall.EINVAL
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bolt.Tx) error {
		cur := string(tx.Bucket(bucketVersioning).Get([]byte(bucket)))
		if cur == string(VersioningSuspended) {
			return syscall.EPERM
		}
		if cur != string(VersioningEnabled) {
			if err := tx.Bucket(bucketVersioning).Put([]byte(bucket), []byte(VersioningEnabled)); err != nil {
				return err
			}
		}
		return tx.Bucket([]byte(bucketObjectLock)).Put([]byte(bucket), []byte{1})
	})
}

// BucketObjectLockEnabled reports whether the bucket is lock-enabled.
func (s *CASStore) BucketObjectLockEnabled(bucket string) (enabled bool, err error) {
	defer common.RecoverErr("CASStore.BucketObjectLockEnabled", &err)
	if bucket == "" {
		return false, syscall.EINVAL
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	err = s.db.View(func(tx *bolt.Tx) error {
		enabled = tx.Bucket([]byte(bucketObjectLock)).Get([]byte(bucket)) != nil
		return nil
	})
	return enabled, err
}

// PutVersionLock writes retention and/or legal-hold state for one version.
// The version record must exist (syscall.ENOENT otherwise). While an active
// COMPLIANCE retention is in the future it can only be extended — never
// weakened to GOVERNANCE, a shorter deadline, or no retention (checked with
// the wall clock, matching S3 request-time semantics for direct calls).
func (s *CASStore) PutVersionLock(bucket, key, versionID string, state ObjectLockState) (err error) {
	defer common.RecoverErr("CASStore.PutVersionLock", &err)
	if bucket == "" || key == "" || versionID == "" {
		return syscall.EINVAL
	}
	if err := validateLockState(state); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bolt.Tx) error {
		var existing ObjectLockState
		if raw := tx.Bucket([]byte(bucketLockStatus)).Get(lockStatusKey(bucket, key, versionID)); raw != nil {
			if err := json.Unmarshal(raw, &existing); err != nil {
				return fmt.Errorf("corrupt lock record: %w", err)
			}
		} else if v := tx.Bucket(bucketObjectVersions).Get(versionKey(bucket, key, versionID)); v == nil {
			return syscall.ENOENT
		}
		if err := checkLockTighten(existing, state); err != nil {
			return err
		}
		nv, err := json.Marshal(state)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketLockStatus)).Put(lockStatusKey(bucket, key, versionID), nv)
	})
}

// validateLockState bounds caller-provided lock fields.
func validateLockState(state ObjectLockState) error {
	switch state.Mode {
	case "", LockModeCompliance, LockModeGovernance:
	default:
		return syscall.EINVAL
	}
	if state.Mode != "" && state.RetainUntil <= 0 {
		return syscall.EINVAL
	}
	return nil
}

// checkLockTighten enforces WORM monotonicity for active COMPLIANCE retention:
// it can only be extended, never shortened, converted to GOVERNANCE, or
// dropped while its deadline is in the future.
func checkLockTighten(existing, next ObjectLockState) error {
	if existing.Mode != LockModeCompliance || existing.RetainUntil <= time.Now().UTC().Unix() {
		return nil
	}
	if next.Mode == LockModeGovernance {
		return syscall.EPERM
	}
	if next.Mode == LockModeCompliance && next.RetainUntil < existing.RetainUntil {
		return syscall.EPERM
	}
	if next.Mode == "" && next.RetainUntil == 0 {
		return syscall.EPERM
	}
	return nil
}

// VersionLock reads the lock record for one version (ok=false when absent).
func (s *CASStore) VersionLock(bucket, key, versionID string) (state ObjectLockState, ok bool, err error) {
	defer common.RecoverErr("CASStore.VersionLock", &err)
	if bucket == "" || key == "" || versionID == "" {
		return state, false, syscall.EINVAL
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	err = s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte(bucketLockStatus)).Get(lockStatusKey(bucket, key, versionID))
		if raw == nil {
			return nil
		}
		if err := json.Unmarshal(raw, &state); err != nil {
			return fmt.Errorf("corrupt lock record: %w", err)
		}
		ok = true
		return nil
	})
	return state, ok, err
}

// lockDeniesTx reports whether the version's lock record denies deletion at
// unix-second time now, honouring the GOVERNANCE bypass flag (bypass never
// defeats a legal hold or COMPLIANCE retention). Caller must hold s.mu inside
// a live update.
func lockDeniesTx(tx *bolt.Tx, bucket, key, versionID string, now int64, bypass bool) (locked bool, err error) {
	raw := tx.Bucket([]byte(bucketLockStatus)).Get(lockStatusKey(bucket, key, versionID))
	if raw == nil {
		return false, nil
	}
	var state ObjectLockState
	if err := json.Unmarshal(raw, &state); err != nil {
		return false, fmt.Errorf("corrupt lock record: %w", err)
	}
	if !state.lockedAt(now) {
		return false, nil
	}
	if bypass && state.Mode == LockModeGovernance && !state.LegalHoldOn {
		return false, nil
	}
	return true, nil
}
