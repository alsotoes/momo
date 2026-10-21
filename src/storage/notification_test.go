package storage

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fastRetries removes the backoff sleep so retry tests run instantly.
func fastRetries(t *testing.T) {
	t.Helper()
	old := notificationRetryBackoff
	notificationRetryBackoff = 0
	t.Cleanup(func() { notificationRetryBackoff = old })
}

// webhookRecorder is an httptest server that records delivered payloads and can
// be told to fail the first n attempts.
type webhookRecorder struct {
	*httptest.Server
	attempts  atomic.Int32
	failFirst int32
	status    int
	bodies    chan s3EventPayload
}

func newWebhookRecorder(failFirst int32, status int) *webhookRecorder {
	w := &webhookRecorder{failFirst: failFirst, status: status, bodies: make(chan s3EventPayload, 16)}
	w.Server = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		n := w.attempts.Add(1)
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if n <= w.failFirst {
			rw.WriteHeader(w.status)
			return
		}
		var p s3EventPayload
		if err := json.Unmarshal(body, &p); err == nil {
			select {
			case w.bodies <- p:
			default:
			}
		}
		rw.WriteHeader(http.StatusOK)
	}))
	return w
}

func TestNotificationConfigCRUD(t *testing.T) {
	s := newTestStore(t)
	bucket := "test-bucket"
	cfg := NotificationConfig{Rules: []NotificationRule{{
		ID: "rule-1", Status: "Enabled",
		Events: []EventType{EventObjectCreatedPut, EventObjectCreatedPost},
		Prefix: "logs/", HTTPUrl: "http://localhost:9000/webhook",
	}}}

	if err := s.SetBucketNotification(bucket, cfg); err != nil {
		t.Fatalf("SetBucketNotification: %v", err)
	}
	got, ok, err := s.BucketNotification(bucket)
	if err != nil || !ok {
		t.Fatalf("BucketNotification: ok=%v err=%v", ok, err)
	}
	if len(got.Rules) != 1 || got.Rules[0].ID != "rule-1" || got.Rules[0].Prefix != "logs/" {
		t.Fatalf("unexpected config round-trip: %+v", got)
	}

	cfg.Rules = append(cfg.Rules, NotificationRule{
		ID: "rule-2", Status: "Enabled",
		Events: []EventType{EventObjectRemovedDelete}, Suffix: ".log",
		HTTPUrl: "http://localhost:9001/webhook",
	})
	if err := s.SetBucketNotification(bucket, cfg); err != nil {
		t.Fatalf("SetBucketNotification update: %v", err)
	}
	got, ok, _ = s.BucketNotification(bucket)
	if !ok || len(got.Rules) != 2 {
		t.Fatalf("expected 2 rules after update, got %d", len(got.Rules))
	}

	if err := s.DeleteBucketNotification(bucket); err != nil {
		t.Fatalf("DeleteBucketNotification: %v", err)
	}
	if _, ok, _ = s.BucketNotification(bucket); ok {
		t.Fatal("expected config to be gone after delete")
	}
}

func TestNotificationEmptyBucket(t *testing.T) {
	s := newTestStore(t)
	cfg := NotificationConfig{Rules: []NotificationRule{{ID: "r1", Status: "Enabled", Events: []EventType{EventObjectCreatedPut}}}}
	if err := s.SetBucketNotification("", cfg); err == nil {
		t.Fatal("expected error for empty bucket")
	}
}

// TestDispatchNotificationDeliversWebhook proves the R10-N1 create scenario:
// a matching event results in an HTTP POST carrying the S3 event document.
func TestDispatchNotificationDeliversWebhook(t *testing.T) {
	fastRetries(t)
	rec := newWebhookRecorder(0, 0)
	defer rec.Close()

	s := newTestStore(t)
	bucket := "test-bucket"
	if err := s.SetBucketNotification(bucket, NotificationConfig{Rules: []NotificationRule{{
		ID: "rule-1", Status: "Enabled",
		Events: []EventType{EventObjectCreatedPut}, Prefix: "logs/", HTTPUrl: rec.URL,
	}}}); err != nil {
		t.Fatalf("SetBucketNotification: %v", err)
	}

	now := time.Unix(1700000000, 0)
	out, err := s.DispatchNotification(bucket, "logs/app.log", "v1", EventObjectCreatedPut, now, 3)
	if err != nil {
		t.Fatalf("DispatchNotification: %v", err)
	}
	if out.Dispatched != 1 || out.Failed != 0 {
		t.Fatalf("expected 1 dispatched / 0 failed, got %+v", out)
	}

	select {
	case p := <-rec.bodies:
		assertCreateRecord(t, p, bucket, "logs/app.log", "v1", now)
	case <-time.After(2 * time.Second):
		t.Fatal("webhook was never called")
	}
}

// assertCreateRecord verifies a delivered ObjectCreated event document.
func assertCreateRecord(t *testing.T, p s3EventPayload, bucket, key, versionID string, now time.Time) {
	t.Helper()
	if len(p.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(p.Records))
	}
	r := p.Records[0]
	if r.EventName != string(EventObjectCreatedPut) {
		t.Fatalf("eventName = %q", r.EventName)
	}
	if r.S3.Bucket.Name != bucket {
		t.Fatalf("bucket name = %q, want %q", r.S3.Bucket.Name, bucket)
	}
	if r.S3.Object.Key != key {
		t.Fatalf("object key = %q, want %q", r.S3.Object.Key, key)
	}
	if r.S3.Object.VersionID != versionID {
		t.Fatalf("versionId = %q, want %q", r.S3.Object.VersionID, versionID)
	}
	if r.EventTime != now.UTC().Format(time.RFC3339) {
		t.Fatalf("eventTime = %q, want the injected now", r.EventTime)
	}
}

// TestDispatchNotificationFiltering proves only matching rules fire.
func TestDispatchNotificationFiltering(t *testing.T) {
	fastRetries(t)
	rec := newWebhookRecorder(0, 0)
	defer rec.Close()

	s := newTestStore(t)
	bucket := "test-bucket"
	if err := s.SetBucketNotification(bucket, NotificationConfig{Rules: []NotificationRule{
		{ID: "prefix", Status: "Enabled", Events: []EventType{EventObjectCreatedPut}, Prefix: "logs/", HTTPUrl: rec.URL},
		{ID: "suffix", Status: "Enabled", Events: []EventType{EventObjectCreatedPut}, Suffix: ".log", HTTPUrl: rec.URL},
		{ID: "disabled", Status: "Disabled", Events: []EventType{EventObjectCreatedPut}, HTTPUrl: rec.URL},
		{ID: "other-event", Status: "Enabled", Events: []EventType{EventObjectRemovedDelete}, HTTPUrl: rec.URL},
	}}); err != nil {
		t.Fatalf("SetBucketNotification: %v", err)
	}

	now := time.Now()
	cases := []struct {
		key, event string
		want       int
	}{
		{"logs/app.log", string(EventObjectCreatedPut), 2}, // prefix + suffix
		{"app.log", string(EventObjectCreatedPut), 1},      // suffix only
		{"other.txt", string(EventObjectCreatedPut), 0},    // neither
		{"logs/app.log", string(EventObjectRemovedDelete), 1},
	}
	for _, tc := range cases {
		out, err := s.DispatchNotification(bucket, tc.key, "v1", EventType(tc.event), now, 1)
		if err != nil {
			t.Fatalf("DispatchNotification(%s,%s): %v", tc.key, tc.event, err)
		}
		if out.Dispatched != tc.want {
			t.Fatalf("%s/%s: dispatched=%d want %d", tc.key, tc.event, out.Dispatched, tc.want)
		}
	}
}

// TestDispatchNotificationRetryBackoff proves the R10-N1 retry scenario: 5xx
// responses are retried up to the configured bound, then succeed.
func TestDispatchNotificationRetryBackoff(t *testing.T) {
	fastRetries(t)
	rec := newWebhookRecorder(2, http.StatusInternalServerError)
	defer rec.Close()

	s := newTestStore(t)
	bucket := "test-bucket"
	if err := s.SetBucketNotification(bucket, NotificationConfig{Rules: []NotificationRule{{
		ID: "r", Status: "Enabled", Events: []EventType{EventObjectCreatedPut}, HTTPUrl: rec.URL,
	}}}); err != nil {
		t.Fatalf("SetBucketNotification: %v", err)
	}

	out, err := s.DispatchNotification(bucket, "k", "", EventObjectCreatedPut, time.Now(), 3)
	if err != nil {
		t.Fatalf("DispatchNotification: %v", err)
	}
	if out.Dispatched != 1 {
		t.Fatalf("expected success after retries, got %+v", out)
	}
	if got := rec.attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts (2 failures + 1 success), got %d", got)
	}
}

// TestDispatchNotificationExhaustedRetries proves a persistently failing webhook
// is counted as Failed, not Dispatched, and does not error the caller.
func TestDispatchNotificationExhaustedRetries(t *testing.T) {
	fastRetries(t)
	rec := newWebhookRecorder(99, http.StatusInternalServerError)
	defer rec.Close()

	s := newTestStore(t)
	bucket := "test-bucket"
	if err := s.SetBucketNotification(bucket, NotificationConfig{Rules: []NotificationRule{{
		ID: "r", Status: "Enabled", Events: []EventType{EventObjectCreatedPut}, HTTPUrl: rec.URL,
	}}}); err != nil {
		t.Fatalf("SetBucketNotification: %v", err)
	}

	out, err := s.DispatchNotification(bucket, "k", "", EventObjectCreatedPut, time.Now(), 2)
	if err != nil {
		t.Fatalf("DispatchNotification must not fail the caller: %v", err)
	}
	if out.Failed != 1 || out.Dispatched != 0 {
		t.Fatalf("expected 1 failed / 0 dispatched, got %+v", out)
	}
	if got := rec.attempts.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

// TestDispatchNotificationClientErrorNoRetry proves a 4xx is not retried.
func TestDispatchNotificationClientErrorNoRetry(t *testing.T) {
	fastRetries(t)
	rec := newWebhookRecorder(99, http.StatusBadRequest)
	defer rec.Close()

	s := newTestStore(t)
	bucket := "test-bucket"
	if err := s.SetBucketNotification(bucket, NotificationConfig{Rules: []NotificationRule{{
		ID: "r", Status: "Enabled", Events: []EventType{EventObjectCreatedPut}, HTTPUrl: rec.URL,
	}}}); err != nil {
		t.Fatalf("SetBucketNotification: %v", err)
	}

	out, _ := s.DispatchNotification(bucket, "k", "", EventObjectCreatedPut, time.Now(), 5)
	if out.Failed != 1 {
		t.Fatalf("expected 1 failed, got %+v", out)
	}
	if got := rec.attempts.Load(); got != 1 {
		t.Fatalf("expected exactly 1 attempt for a 4xx, got %d", got)
	}
}

// TestDispatchNotificationUnsupportedTarget proves a rule with only a non-HTTP
// destination fails explicitly rather than silently succeeding.
func TestDispatchNotificationUnsupportedTarget(t *testing.T) {
	fastRetries(t)
	s := newTestStore(t)
	bucket := "test-bucket"
	if err := s.SetBucketNotification(bucket, NotificationConfig{Rules: []NotificationRule{{
		ID: "sqs", Status: "Enabled", Events: []EventType{EventObjectCreatedPut},
		QueueARN: "arn:aws:sqs:us-east-1:1:q",
	}}}); err != nil {
		t.Fatalf("SetBucketNotification: %v", err)
	}
	out, err := s.DispatchNotification(bucket, "k", "", EventObjectCreatedPut, time.Now(), 1)
	if err != nil {
		t.Fatalf("DispatchNotification: %v", err)
	}
	if out.Failed != 1 || out.Dispatched != 0 {
		t.Fatalf("expected unsupported target to fail, got %+v", out)
	}
}

func TestDispatchNotificationNoConfig(t *testing.T) {
	s := newTestStore(t)
	out, err := s.DispatchNotification("no-such-bucket", "key", "v1", EventObjectCreatedPut, time.Now(), 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Dispatched != 0 || out.Failed != 0 {
		t.Fatalf("expected no-op, got %+v", out)
	}
}

func TestRuleMatches(t *testing.T) {
	cases := []struct {
		name  string
		rule  NotificationRule
		key   string
		event EventType
		want  bool
	}{
		{"disabled", NotificationRule{Status: "Disabled", Events: []EventType{EventObjectCreatedPut}}, "k", EventObjectCreatedPut, false},
		{"event-mismatch", NotificationRule{Status: "Enabled", Events: []EventType{EventObjectCreatedPut}}, "k", EventObjectRemovedDelete, false},
		{"event-match", NotificationRule{Status: "Enabled", Events: []EventType{EventObjectCreatedPut}}, "k", EventObjectCreatedPut, true},
		{"prefix-miss", NotificationRule{Status: "Enabled", Events: []EventType{EventObjectCreatedPut}, Prefix: "logs/"}, "app.log", EventObjectCreatedPut, false},
		{"prefix-hit", NotificationRule{Status: "Enabled", Events: []EventType{EventObjectCreatedPut}, Prefix: "logs/"}, "logs/app.log", EventObjectCreatedPut, true},
		{"suffix-miss", NotificationRule{Status: "Enabled", Events: []EventType{EventObjectCreatedPut}, Suffix: ".log"}, "app.txt", EventObjectCreatedPut, false},
		{"suffix-hit", NotificationRule{Status: "Enabled", Events: []EventType{EventObjectCreatedPut}, Suffix: ".log"}, "app.log", EventObjectCreatedPut, true},
		{"exact-suffix", NotificationRule{Status: "Enabled", Events: []EventType{EventObjectCreatedPut}, Suffix: ".log"}, ".log", EventObjectCreatedPut, true},
	}
	for _, tc := range cases {
		if got := ruleMatches(tc.rule, tc.key, tc.event); got != tc.want {
			t.Errorf("%s: ruleMatches = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBuildS3EventPayload(t *testing.T) {
	now := time.Unix(1700000000, 0)
	p := buildS3EventPayload("b", "k", "v9", EventObjectRemovedDelete, now)
	if len(p.Records) != 1 {
		t.Fatalf("expected 1 record")
	}
	r := p.Records[0]
	if r.EventSource != "aws:s3" || r.EventVersion != "2.1" {
		t.Fatalf("unexpected envelope: %+v", r)
	}
	if r.S3.Bucket.Name != "b" || r.S3.Object.Key != "k" || r.S3.Object.VersionID != "v9" {
		t.Fatalf("unexpected entity: %+v", r.S3)
	}
	if r.EventTime != now.UTC().Format(time.RFC3339) {
		t.Fatalf("eventTime = %q", r.EventTime)
	}
}
