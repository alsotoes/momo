package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"syscall"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/alsotoes/momo/src/common"
)

// EventType represents an S3 event type that can trigger a notification.
type EventType string

const (
	EventObjectCreatedPut          EventType = "s3:ObjectCreated:Put"
	EventObjectCreatedPost         EventType = "s3:ObjectCreated:Post"
	EventObjectCreatedCopy         EventType = "s3:ObjectCreated:Copy"
	EventObjectCreatedMultipart    EventType = "s3:ObjectCreated:CompleteMultipartUpload"
	EventObjectRemovedDelete       EventType = "s3:ObjectRemoved:Delete"
	EventObjectRemovedDeleteMarker EventType = "s3:ObjectRemoved:DeleteMarkerCreated"
)

// notificationRetryBackoff is the base delay between webhook delivery attempts.
// Each subsequent attempt doubles it (exponential backoff). It is a variable so
// tests can set it to zero.
var notificationRetryBackoff = 100 * time.Millisecond

// notificationHTTPTimeout bounds a single webhook delivery attempt.
const notificationHTTPTimeout = 10 * time.Second

// NotificationRule represents one S3 notification rule.
type NotificationRule struct {
	ID        string      `json:"id"`
	Status    string      `json:"status"` // Enabled | Disabled
	Events    []EventType `json:"events"`
	Prefix    string      `json:"prefix,omitempty"`
	Suffix    string      `json:"suffix,omitempty"`
	QueueARN  string      `json:"queue_arn,omitempty"`  // SQS
	TopicARN  string      `json:"topic_arn,omitempty"`  // SNS
	LambdaARN string      `json:"lambda_arn,omitempty"` // Lambda
	HTTPUrl   string      `json:"http_url,omitempty"`   // Webhook
}

// NotificationConfig is the ordered rule set for one bucket.
type NotificationConfig struct {
	Rules []NotificationRule `json:"rules"`
}

// NotificationOutcome reports what a single notification dispatch did.
type NotificationOutcome struct {
	Dispatched int
	Failed     int
}

// SetBucketNotification stores the notification configuration for bucket.
func (s *CASStore) SetBucketNotification(bucket string, cfg NotificationConfig) (err error) {
	defer common.RecoverErr("CASStore.SetBucketNotification", &err)
	if bucket == "" {
		return fmt.Errorf("notification: empty bucket: %w", syscall.EINVAL)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("notification: encode config: %w: %v", syscall.EIO, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketNotification).Put([]byte(bucket), data)
	})
}

// BucketNotification returns the stored notification configuration for bucket
// and whether one exists.
func (s *CASStore) BucketNotification(bucket string) (cfg NotificationConfig, ok bool, err error) {
	defer common.RecoverErr("CASStore.BucketNotification", &err)
	s.mu.RLock()
	defer s.mu.RUnlock()
	err = s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketNotification).Get([]byte(bucket))
		if raw == nil {
			return nil
		}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("notification: decode config for %s: %w: %v", bucket, syscall.EIO, err)
		}
		ok = true
		return nil
	})
	return cfg, ok, err
}

// DeleteBucketNotification removes the notification configuration for bucket.
func (s *CASStore) DeleteBucketNotification(bucket string) (err error) {
	defer common.RecoverErr("CASStore.DeleteBucketNotification", &err)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketNotification).Delete([]byte(bucket))
	})
}

// DispatchNotification delivers eventType for bucket/key to every matching rule.
// now is the event time (injected for deterministic tests); retryMax bounds the
// webhook delivery attempts per rule. A failing rule increments Failed and does
// not abort delivery to the remaining rules.
func (s *CASStore) DispatchNotification(bucket, key, versionID string, eventType EventType, now time.Time, retryMax int) (out NotificationOutcome, err error) {
	defer common.RecoverErr("CASStore.DispatchNotification", &err)

	cfg, ok, err := s.BucketNotification(bucket)
	if err != nil || !ok || len(cfg.Rules) == 0 {
		return out, err
	}

	for _, rule := range cfg.Rules {
		if !ruleMatches(rule, key, eventType) {
			continue
		}
		if derr := dispatchToTarget(rule, bucket, key, versionID, eventType, now, retryMax); derr != nil {
			log.Printf("AUDIT: notification rule %s delivery failed for %s: %v", rule.ID, key, common.SanitizeLog(derr.Error()))
			out.Failed++
			continue
		}
		out.Dispatched++
	}
	return out, nil
}

// ruleMatches reports whether a rule fires for the given key and event type.
func ruleMatches(rule NotificationRule, key string, eventType EventType) bool {
	if rule.Status != "Enabled" {
		return false
	}
	eventMatched := false
	for _, et := range rule.Events {
		if et == eventType {
			eventMatched = true
			break
		}
	}
	if !eventMatched {
		return false
	}
	if rule.Prefix != "" && !strings.HasPrefix(key, rule.Prefix) {
		return false
	}
	if rule.Suffix != "" && !strings.HasSuffix(key, rule.Suffix) {
		return false
	}
	return true
}

// s3EventPayload is the S3 event notification document posted to a webhook.
type s3EventPayload struct {
	Records []s3EventRecord `json:"Records"`
}

type s3EventRecord struct {
	EventVersion string        `json:"eventVersion"`
	EventSource  string        `json:"eventSource"`
	EventTime    string        `json:"eventTime"`
	EventName    string        `json:"eventName"`
	S3           s3EventEntity `json:"s3"`
}

type s3EventEntity struct {
	Bucket s3EventBucket `json:"bucket"`
	Object s3EventObject `json:"object"`
}

type s3EventBucket struct {
	Name string `json:"name"`
}

type s3EventObject struct {
	Key       string `json:"key"`
	VersionID string `json:"versionId,omitempty"`
}

// buildS3EventPayload constructs the S3 event document for one event.
func buildS3EventPayload(bucket, key, versionID string, eventType EventType, now time.Time) s3EventPayload {
	return s3EventPayload{
		Records: []s3EventRecord{{
			EventVersion: "2.1",
			EventSource:  "aws:s3",
			EventTime:    now.UTC().Format(time.RFC3339),
			EventName:    string(eventType),
			S3: s3EventEntity{
				Bucket: s3EventBucket{Name: bucket},
				Object: s3EventObject{Key: key, VersionID: versionID},
			},
		}},
	}
}

// dispatchToTarget delivers one event to the rule's target. Only HTTP webhooks
// are supported; SQS/SNS/Lambda return an explicit not-implemented error rather
// than silently succeeding.
func dispatchToTarget(rule NotificationRule, bucket, key, versionID string, eventType EventType, now time.Time, retryMax int) error {
	if rule.HTTPUrl == "" {
		return fmt.Errorf("notification rule %s: only HttpUrl webhooks are supported: %w", rule.ID, syscall.ENOSYS)
	}
	payload, err := json.Marshal(buildS3EventPayload(bucket, key, versionID, eventType, now))
	if err != nil {
		return fmt.Errorf("notification rule %s: encode payload: %w: %v", rule.ID, syscall.EIO, err)
	}
	return dispatchHTTP(rule.HTTPUrl, payload, retryMax)
}

// dispatchHTTP POSTs payload to url, retrying with exponential backoff up to
// retryMax attempts. Network errors and 5xx responses are retried; a 4xx
// response is returned immediately (retrying cannot help a rejected request).
func dispatchHTTP(url string, payload []byte, retryMax int) error {
	if retryMax < 1 {
		retryMax = 1
	}
	client := &http.Client{Timeout: notificationHTTPTimeout}
	var lastErr error
	for attempt := 0; attempt < retryMax; attempt++ {
		if attempt > 0 {
			time.Sleep(notificationRetryBackoff << (attempt - 1))
		}
		resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
		if err != nil {
			lastErr = err
			continue
		}
		// Drain and close so the connection can be reused; the body is not read.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return nil
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			return fmt.Errorf("notification webhook returned %d: %w", resp.StatusCode, syscall.EIO)
		default:
			lastErr = fmt.Errorf("notification webhook returned %d", resp.StatusCode)
		}
	}
	return fmt.Errorf("notification webhook delivery failed after %d attempts: %w", retryMax, lastErr)
}
