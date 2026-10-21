package storage

import (
	"testing"
	"time"
)

func TestNotificationConfigCRUD(t *testing.T) {
	s := newTestStore(t)

	bucket := "test-bucket"
	cfg := NotificationConfig{
		Rules: []NotificationRule{
			{
				ID:      "rule-1",
				Status:  "Enabled",
				Events:  []EventType{EventObjectCreatedPut, EventObjectCreatedPost},
				Prefix:  "logs/",
				HTTPUrl: "http://localhost:9000/webhook",
			},
		},
	}

	// Set
	if err := s.SetBucketNotification(bucket, cfg); err != nil {
		t.Fatalf("SetBucketNotification: %v", err)
	}

	// Get
	got, ok, err := s.BucketNotification(bucket)
	if err != nil {
		t.Fatalf("BucketNotification error: %v", err)
	}
	if !ok {
		t.Fatal("expected notification config to exist")
	}
	if len(got.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(got.Rules))
	}
	if got.Rules[0].ID != "rule-1" {
		t.Fatalf("expected rule-1, got %s", got.Rules[0].ID)
	}
	if got.Rules[0].Prefix != "logs/" {
		t.Fatalf("expected prefix logs/, got %s", got.Rules[0].Prefix)
	}

	// Update
	cfg.Rules = append(cfg.Rules, NotificationRule{
		ID:      "rule-2",
		Status:  "Enabled",
		Events:  []EventType{EventObjectRemovedDelete},
		Suffix:  ".log",
		HTTPUrl: "http://localhost:9001/webhook",
	})
	if err := s.SetBucketNotification(bucket, cfg); err != nil {
		t.Fatalf("SetBucketNotification update: %v", err)
	}

	got, ok, err = s.BucketNotification(bucket)
	if err != nil {
		t.Fatalf("BucketNotification after update: %v", err)
	}
	if !ok || len(got.Rules) != 2 {
		t.Fatalf("expected 2 rules after update, got %d", len(got.Rules))
	}

	// Delete
	if err := s.DeleteBucketNotification(bucket); err != nil {
		t.Fatalf("DeleteBucketNotification: %v", err)
	}
	_, ok, err = s.BucketNotification(bucket)
	if err != nil {
		t.Fatalf("BucketNotification after delete error: %v", err)
	}
	if ok {
		t.Fatal("expected notification config to not exist after delete")
	}
}

func TestNotificationEmptyBucket(t *testing.T) {
	s := newTestStore(t)
	// Empty bucket name should fail
	cfg := NotificationConfig{Rules: []NotificationRule{{ID: "r1", Status: "Enabled", Events: []EventType{EventObjectCreatedPut}}}}
	if err := s.SetBucketNotification("", cfg); err == nil {
		t.Fatal("expected error for empty bucket")
	}
}

func TestDispatchNotification(t *testing.T) {
	s := newTestStore(t)
	bucket := "test-bucket"
	cfg := NotificationConfig{
		Rules: []NotificationRule{
			{
				ID:      "rule-1",
				Status:  "Enabled",
				Events:  []EventType{EventObjectCreatedPut},
				Prefix:  "logs/",
				HTTPUrl: "http://localhost:9000/webhook", // Will fail but we test the matching
			},
			{
				ID:      "rule-2",
				Status:  "Enabled",
				Events:  []EventType{EventObjectCreatedPut},
				Suffix:  ".log",
				HTTPUrl: "http://localhost:9001/webhook",
			},
			{
				ID:      "rule-3",
				Status:  "Disabled",
				Events:  []EventType{EventObjectCreatedPut},
				HTTPUrl: "http://localhost:9002/webhook",
			},
		},
	}
	if err := s.SetBucketNotification(bucket, cfg); err != nil {
		t.Fatalf("SetBucketNotification: %v", err)
	}

	now := time.Now()
	// Match prefix rule AND suffix rule (both match "logs/app.log")
	out, err := s.DispatchNotification(bucket, "logs/app.log", "v1", EventObjectCreatedPut, now)
	if err != nil {
		t.Fatalf("DispatchNotification error: %v", err)
	}
	if out.Dispatched != 2 {
		t.Fatalf("expected 2 dispatched for prefix+suffix match, got %d", out.Dispatched)
	}

	// Match suffix rule only
	out, err = s.DispatchNotification(bucket, "app.log", "v1", EventObjectCreatedPut, now)
	if err != nil {
		t.Fatalf("DispatchNotification error: %v", err)
	}
	if out.Dispatched != 1 {
		t.Fatalf("expected 1 dispatched for suffix match, got %d", out.Dispatched)
	}

	// Match neither
	out, err = s.DispatchNotification(bucket, "other.txt", "v1", EventObjectCreatedPut, now)
	if err != nil {
		t.Fatalf("DispatchNotification error: %v", err)
	}
	if out.Dispatched != 0 {
		t.Fatalf("expected 0 dispatched for no match, got %d", out.Dispatched)
	}

	// Disabled rule should not match
	out, err = s.DispatchNotification(bucket, "logs/app.log", "v1", EventObjectCreatedPost, now)
	if err != nil {
		t.Fatalf("DispatchNotification error: %v", err)
	}
	if out.Dispatched != 0 {
		t.Fatalf("expected 0 dispatched for disabled rule, got %d", out.Dispatched)
	}

	// No config should return 0
	_, ok, _ := s.BucketNotification("no-config-bucket")
	if ok {
		t.Fatal("expected no config for no-config-bucket")
	}
	out, err = s.DispatchNotification("no-config-bucket", "logs/app.log", "v1", EventObjectCreatedPut, now)
	if err != nil {
		t.Fatalf("DispatchNotification error: %v", err)
	}
	if out.Dispatched != 0 {
		t.Fatalf("expected 0 dispatched for no config, got %d", out.Dispatched)
	}
}

func TestEventTypeMatching(t *testing.T) {
	s := newTestStore(t)
	bucket := "test-bucket"
	cfg := NotificationConfig{
		Rules: []NotificationRule{
			{
				ID:     "rule-1",
				Status: "Enabled",
				Events: []EventType{
					EventObjectCreatedPut,
					EventObjectCreatedPost,
					EventObjectCreatedCopy,
					EventObjectCreatedMultipart,
					EventObjectRemovedDelete,
					EventObjectRemovedDeleteMarker,
				},
				HTTPUrl: "http://localhost:9000/webhook",
			},
		},
	}
	if err := s.SetBucketNotification(bucket, cfg); err != nil {
		t.Fatalf("SetBucketNotification: %v", err)
	}

	now := time.Now()
	events := []EventType{
		EventObjectCreatedPut,
		EventObjectCreatedPost,
		EventObjectCreatedCopy,
		EventObjectCreatedMultipart,
		EventObjectRemovedDelete,
		EventObjectRemovedDeleteMarker,
	}
	for _, et := range events {
		out, err := s.DispatchNotification(bucket, "test.key", "v1", et, now)
		if err != nil {
			t.Fatalf("DispatchNotification error for %s: %v", et, err)
		}
		if out.Dispatched != 1 {
			t.Fatalf("expected 1 dispatched for %s, got %d", et, out.Dispatched)
		}
	}
}

func TestDispatchNotificationNoConfig(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	out, err := s.DispatchNotification("no-such-bucket", "key", "v1", EventObjectCreatedPut, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Dispatched != 0 {
		t.Fatalf("expected 0 dispatched, got %d", out.Dispatched)
	}
}
