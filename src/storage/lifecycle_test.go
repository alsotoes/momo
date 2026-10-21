package storage

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

func putTestObject(t *testing.T, s *CASStore, name string) {
	t.Helper()
	payload := []byte("payload-" + name)
	sum := sha256.Sum256(payload)
	hash := fmt.Sprintf("%x", sum)
	if err := s.Put(name, hash, int64(len(payload)), "", bytes.NewReader(payload)); err != nil {
		t.Fatalf("Put(%s): %v", name, err)
	}
}

// objectExists reports whether name resolves to stored content.
func objectExists(t *testing.T, s *CASStore, name string) bool {
	t.Helper()
	rc, _, err := s.Get(name)
	if err != nil {
		return false
	}
	_ = rc.Close()
	return true
}

func TestBucketLifecycleConfigCRUD(t *testing.T) {
	s := newTestStore(t)

	if _, ok, err := s.BucketLifecycle("b1"); err != nil || ok {
		t.Fatalf("expected no config, ok=%v err=%v", ok, err)
	}

	cfg := LifecycleConfig{Rules: []LifecycleRule{
		{ID: "expire-logs", Status: LifecycleEnabled, Prefix: "logs/", ExpirationDays: 30},
		{ID: "archive", Status: LifecycleEnabled, Prefix: "data/", TransitionDays: 7, StorageClass: "STANDARD_IA"},
	}}
	if err := s.SetBucketLifecycle("b1", cfg); err != nil {
		t.Fatalf("SetBucketLifecycle: %v", err)
	}
	got, ok, err := s.BucketLifecycle("b1")
	if err != nil || !ok {
		t.Fatalf("BucketLifecycle: ok=%v err=%v", ok, err)
	}
	if len(got.Rules) != 2 || got.Rules[0].ID != "expire-logs" || got.Rules[1].StorageClass != "STANDARD_IA" {
		t.Fatalf("unexpected config round-trip: %+v", got)
	}

	if err := s.DeleteBucketLifecycle("b1"); err != nil {
		t.Fatalf("DeleteBucketLifecycle: %v", err)
	}
	if _, ok, err := s.BucketLifecycle("b1"); err != nil || ok {
		t.Fatalf("expected config removed, ok=%v err=%v", ok, err)
	}
}

func TestSetBucketLifecycleEmptyBucketRejected(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetBucketLifecycle("", LifecycleConfig{Rules: []LifecycleRule{{ID: "x", Status: LifecycleEnabled, ExpirationDays: 1}}}); err == nil {
		t.Fatal("expected an error for an empty bucket name")
	}
}

func TestEvaluateLifecycleExpiration(t *testing.T) {
	s := newTestStore(t)
	putTestObject(t, s, "logs/app.log")
	putTestObject(t, s, "keep/app.log")

	if err := s.SetBucketLifecycle("b1", LifecycleConfig{Rules: []LifecycleRule{
		{ID: "expire-logs", Status: LifecycleEnabled, Prefix: "logs/", ExpirationDays: 30},
	}}); err != nil {
		t.Fatalf("SetBucketLifecycle: %v", err)
	}

	future := time.Now().Add(100 * 24 * time.Hour)
	out, err := s.EvaluateLifecycle("b1", future)
	if err != nil {
		t.Fatalf("EvaluateLifecycle: %v", err)
	}
	if out.Expired != 1 || out.Transitioned != 0 {
		t.Fatalf("expected 1 expired, got %+v", out)
	}

	// The matching object is gone; the non-matching one survives.
	if objectExists(t, s, "logs/app.log") {
		t.Fatal("expected logs/app.log to be expired")
	}
	if !objectExists(t, s, "keep/app.log") {
		t.Fatal("keep/app.log should survive")
	}
}

func TestEvaluateLifecycleTransition(t *testing.T) {
	s := newTestStore(t)
	putTestObject(t, s, "data/blob.bin")

	if err := s.SetBucketLifecycle("b1", LifecycleConfig{Rules: []LifecycleRule{
		{ID: "archive", Status: LifecycleEnabled, Prefix: "data/", TransitionDays: 7, StorageClass: "STANDARD_IA"},
	}}); err != nil {
		t.Fatalf("SetBucketLifecycle: %v", err)
	}

	future := time.Now().Add(30 * 24 * time.Hour)
	out, err := s.EvaluateLifecycle("b1", future)
	if err != nil {
		t.Fatalf("EvaluateLifecycle: %v", err)
	}
	if out.Transitioned != 1 || out.Expired != 0 {
		t.Fatalf("expected 1 transitioned, got %+v", out)
	}
	if got := s.GetS3Meta("data/blob.bin")["x-amz-storage-class"]; got != "STANDARD_IA" {
		t.Fatalf("expected storage class STANDARD_IA, got %q", got)
	}
}

func TestEvaluateLifecycleVersionedExpirationWritesMarker(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetBucketVersioning("b1", VersioningEnabled); err != nil {
		t.Fatalf("SetBucketVersioning: %v", err)
	}
	putTestObject(t, s, "logs/app.log")

	if err := s.SetBucketLifecycle("b1", LifecycleConfig{Rules: []LifecycleRule{
		{ID: "expire-logs", Status: LifecycleEnabled, Prefix: "logs/", ExpirationDays: 1},
	}}); err != nil {
		t.Fatalf("SetBucketLifecycle: %v", err)
	}

	out, err := s.EvaluateLifecycle("b1", time.Now().Add(10*24*time.Hour))
	if err != nil {
		t.Fatalf("EvaluateLifecycle: %v", err)
	}
	if out.Expired != 1 {
		t.Fatalf("expected 1 expired, got %+v", out)
	}
	rec, ok, err := s.LatestVersion("b1", "logs/app.log")
	if err != nil || !ok {
		t.Fatalf("LatestVersion: ok=%v err=%v", ok, err)
	}
	if !rec.IsDeleteMarker {
		t.Fatalf("expected a delete marker as the latest version, got %+v", rec)
	}
}

func TestEvaluateLifecycleDisabledRuleIsInert(t *testing.T) {
	s := newTestStore(t)
	putTestObject(t, s, "logs/app.log")

	if err := s.SetBucketLifecycle("b1", LifecycleConfig{Rules: []LifecycleRule{
		{ID: "off", Status: LifecycleDisabled, Prefix: "logs/", ExpirationDays: 1},
	}}); err != nil {
		t.Fatalf("SetBucketLifecycle: %v", err)
	}
	out, err := s.EvaluateLifecycle("b1", time.Now().Add(1000*24*time.Hour))
	if err != nil {
		t.Fatalf("EvaluateLifecycle: %v", err)
	}
	if out.Expired != 0 {
		t.Fatalf("disabled rule should be inert, got %+v", out)
	}
	if !objectExists(t, s, "logs/app.log") {
		t.Fatal("object should survive a disabled rule")
	}
}

func TestEvaluateLifecycleNoConfigIsNoOp(t *testing.T) {
	s := newTestStore(t)
	putTestObject(t, s, "a")
	out, err := s.EvaluateLifecycle("noconfig", time.Now().Add(1000*24*time.Hour))
	if err != nil {
		t.Fatalf("EvaluateLifecycle: %v", err)
	}
	if out.Expired != 0 || out.Transitioned != 0 {
		t.Fatalf("expected no-op, got %+v", out)
	}
}
