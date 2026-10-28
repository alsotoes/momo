package storage

// Object-lock storage tests (R10-LK1). The delete-denial clock is injected
// through DeleteVersion's now parameter; COMPLIANCE tighten checks use the
// wall clock, so possession-of-record assertions there use retained windows
// far enough out to stay future at test execution time.

import (
	"errors"
	"syscall"
	"testing"
	"time"
)

// future returns a retention deadline far enough ahead for COMPLIANCE
// monotonicity checks to see it as active.
func future() int64 { return time.Now().UTC().Add(24 * time.Hour).Unix() }

func TestSetBucketObjectLock_EnablesVersioning(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetBucketObjectLock("b1"); err != nil {
		t.Fatalf("SetBucketObjectLock: %v", err)
	}
	enabled, err := s.BucketObjectLockEnabled("b1")
	if err != nil || !enabled {
		t.Fatalf("expected lock-enabled, enabled=%v err=%v", enabled, err)
	}
	status, _, err := s.BucketVersioning("b1")
	if err != nil || status != VersioningEnabled {
		t.Fatalf("lock must imply versioning enabled, status=%q err=%v", status, err)
	}
}

func TestSetBucketObjectLock_Refusals(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetBucketObjectLock(""); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("empty bucket: want EINVAL, got %v", err)
	}
	if err := s.SetBucketVersioning("sb", VersioningSuspended); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	// Last write intentionally disables the bucket after the lock refusal:
	// a lock-enabled bucket must never accept a suspension.
	if err := s.SetBucketObjectLock("sb"); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("suspended bucket: want EPERM, got %v", err)
	}
	if enabled, _ := s.BucketObjectLockEnabled("sb"); enabled {
		t.Fatalf("refused lock must not persist state")
	}
	if err := s.SetBucketVersioning("sb", VersioningEnabled); err != nil {
		t.Fatalf("re-enable for lock: %v", err)
	}
	if err := s.SetBucketVersioning("sb", VersioningSuspended); err != nil {
		t.Fatalf("versioning API must stay reachable without lock: %v", err)
	}
}

func TestPutVersionLock_Validations(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetBucketObjectLock("b"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	id, err := s.RecordVersion("b", "k", "hash-1", 10)
	if err != nil {
		t.Fatalf("RecordVersion: %v", err)
	}
	if err := s.PutVersionLock("b", "k", "does-not-exist", ObjectLockState{Mode: LockModeCompliance, RetainUntil: future()}); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("unknown version: want ENOENT, got %v", err)
	}
	if err := s.PutVersionLock("b", "k", id, ObjectLockState{Mode: "PARANOID", RetainUntil: future()}); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("bad mode: want EINVAL, got %v", err)
	}
	if err := s.PutVersionLock("b", "k", id, ObjectLockState{Mode: LockModeCompliance, RetainUntil: 0}); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("retention without deadline: want EINVAL, got %v", err)
	}
	if err := s.PutVersionLock("b", "", id, ObjectLockState{}); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("empty key: want EINVAL, got %v", err)
	}
}

func TestVersionLock_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetBucketObjectLock("b"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	id, err := s.RecordVersion("b", "k", "hash-1", 10)
	if err != nil {
		t.Fatalf("RecordVersion: %v", err)
	}
	if _, ok, err := s.VersionLock("b", "k", id); err != nil || ok {
		t.Fatalf("expected no lock record, ok=%v err=%v", ok, err)
	}
	want := ObjectLockState{Mode: LockModeGovernance, RetainUntil: future(), LegalHoldOn: false}
	if err := s.PutVersionLock("b", "k", id, want); err != nil {
		t.Fatalf("PutVersionLock: %v", err)
	}
	got, ok, err := s.VersionLock("b", "k", id)
	if err != nil || !ok {
		t.Fatalf("expected lock record, ok=%v err=%v", ok, err)
	}
	if got != want {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
	}
}

func TestDeleteVersion_LockDenials(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetBucketObjectLock("b"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	idComp, err := s.RecordVersion("b", "compliance", "hash-c", 10)
	if err != nil {
		t.Fatalf("RecordVersion compliance: %v", err)
	}
	if err := s.PutVersionLock("b", "compliance", idComp, ObjectLockState{Mode: LockModeCompliance, RetainUntil: future()}); err != nil {
		t.Fatalf("PutVersionLock compliance: %v", err)
	}
	vGoid, err := s.RecordVersion("b", "governance", "hash-g", 10)
	if err != nil {
		t.Fatalf("RecordVersion governance: %v", err)
	}
	if err := s.PutVersionLock("b", "governance", vGoid, ObjectLockState{Mode: LockModeGovernance, RetainUntil: future()}); err != nil {
		t.Fatalf("PutVersionLock governance: %v", err)
	}
	idHold, err := s.RecordVersion("b", "hold", "hash-h", 10)
	if err != nil {
		t.Fatalf("RecordVersion hold: %v", err)
	}
	if err := s.PutVersionLock("b", "hold", idHold, ObjectLockState{LegalHoldOn: true}); err != nil {
		t.Fatalf("PutVersionLock hold: %v", err)
	}

	now := time.Now().UTC().Unix()

	// COMPLIANCE is absolute.
	if _, err := s.DeleteVersion("b", "compliance", idComp, now, false); !errors.Is(err, syscall.EACCES) {
		t.Fatalf("compliance delete: want EACCES, got %v", err)
	}
	if _, err := s.DeleteVersion("b", "compliance", idComp, now, true); !errors.Is(err, syscall.EACCES) {
		t.Fatalf("compliance with bypass must still deny: want EACCES, got %v", err)
	}
	// After expiry the record itself removes without error.
	if _, err := s.DeleteVersion("b", "compliance", idComp, future()+1, false); err != nil {
		t.Fatalf("expired compliance must be deletable: %v", err)
	}
	// GOVERNANCE honours the bypass flag, never a legal hold.
	if _, err := s.DeleteVersion("b", "governance", vGoid, now, true); err != nil {
		t.Fatalf("governance bypass must bypass: %v", err)
	}
	idGov2, err := s.RecordVersion("b", "governance2", "hash-g2", 10)
	if err != nil {
		t.Fatalf("RecordVersion governance2: %v", err)
	}
	if err := s.PutVersionLock("b", "governance2", idGov2, ObjectLockState{Mode: LockModeGovernance, RetainUntil: future(), LegalHoldOn: true}); err != nil {
		t.Fatalf("PutVersionLock hold+gov: %v", err)
	}
	if _, err := s.DeleteVersion("b", "governance2", idGov2, now, true); !errors.Is(err, syscall.EACCES) {
		t.Fatalf("legal hold must survive bypass: want EACCES, got %v", err)
	}
}

func TestDeleteVersion_Unprotected(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetBucketObjectLock("b"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	id, err := s.RecordVersion("b", "free", "hash-f", 10)
	if err != nil {
		t.Fatalf("RecordVersion: %v", err)
	}
	if _, err := s.DeleteVersion("b", "free", id, time.Now().UTC().Unix(), false); err != nil {
		t.Fatalf("unlocked version must delete: %v", err)
	}
	// Overwrite of a locked key stays allowed (new version); only deletion
	// is denied — decision recorded in objectlock.go.
	idLocked, err := s.RecordVersion("b", "k", "hash-1", 10)
	if err != nil {
		t.Fatalf("RecordVersion: %v", err)
	}
	if err := s.PutVersionLock("b", "k", idLocked, ObjectLockState{LegalHoldOn: true}); err != nil {
		t.Fatalf("PutVersionLock: %v", err)
	}
	if _, err := s.RecordVersion("b", "k", "hash-2", 20); err != nil {
		t.Fatalf("overwrite of locked key must be allowed: %v", err)
	}
}

func TestPutVersionLock_ComplianceTightenOnly(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetBucketObjectLock("b"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	id, err := s.RecordVersion("b", "k", "hash-1", 10)
	if err != nil {
		t.Fatalf("RecordVersion: %v", err)
	}
	base := time.Now().UTC().Add(72 * time.Hour).Unix()
	if err := s.PutVersionLock("b", "k", id, ObjectLockState{Mode: LockModeCompliance, RetainUntil: base}); err != nil {
		t.Fatalf("initial: %v", err)
	}
	// Extend: allowed.
	if err := s.PutVersionLock("b", "k", id, ObjectLockState{Mode: LockModeCompliance, RetainUntil: base + 3600}); err != nil {
		t.Fatalf("extension must be allowed: %v", err)
	}
	// Shorten: refused.
	if err := s.PutVersionLock("b", "k", id, ObjectLockState{Mode: LockModeCompliance, RetainUntil: base - 3600}); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("shorten: want EPERM, got %v", err)
	}
	// Downgrade: refused.
	if err := s.PutVersionLock("b", "k", id, ObjectLockState{Mode: LockModeGovernance, RetainUntil: base + 3600}); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("downgrade: want EPERM, got %v", err)
	}
	// Drop retention: refused.
	if err := s.PutVersionLock("b", "k", id, ObjectLockState{LegalHoldOn: true}); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("drop: want EPERM, got %v", err)
	}
	// No record at all: refused (implicit drop).
	if err := s.PutVersionLock("b", "k", id, ObjectLockState{}); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("drop to nothing: want EPERM, got %v", err)
	}
}
