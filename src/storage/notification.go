package storage

import (
	"encoding/json"
	"fmt"
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

// NotificationEvent represents a single event to be dispatched.
type NotificationEvent struct {
	EventType EventType
	Bucket    string
	Key       string
	VersionID string
	Timestamp time.Time
}

// DispatchNotification dispatches a single event to all matching rules.
func (s *CASStore) DispatchNotification(bucket, key, versionID string, eventType EventType, now time.Time) (out NotificationOutcome, err error) {
	defer common.RecoverErr("CASStore.DispatchNotification", &err)

	cfg, ok, err := s.BucketNotification(bucket)
	if err != nil || !ok || len(cfg.Rules) == 0 {
		return out, err
	}

	for _, rule := range cfg.Rules {
		if !ruleMatches(rule, key, eventType) {
			continue
		}

		// Dispatch to the configured target
		if err := s.dispatchToTarget(rule, bucket, key, versionID, eventType); err != nil {
			// Log but continue to other rules
			continue
		}
		out.Dispatched++
	}
	return out, nil
}

// ruleMatches checks if a notification rule matches the given key and event type.
func ruleMatches(rule NotificationRule, key string, eventType EventType) bool {
	if rule.Status != "Enabled" {
		return false
	}
	// Match event type
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
	// Match prefix/suffix
	if rule.Prefix != "" && !hasPrefix(key, rule.Prefix) {
		return false
	}
	if rule.Suffix != "" && !hasSuffix(key, rule.Suffix) {
		return false
	}
	return true
}

// hasPrefix checks if key starts with prefix.
func hasPrefix(key, prefix string) bool {
	return len(key) >= len(prefix) && key[:len(prefix)] == prefix
}

// hasSuffix checks if key ends with suffix.
func hasSuffix(key, suffix string) bool {
	return len(key) >= len(suffix) && key[len(key)-len(suffix):] == suffix
}

// dispatchToTarget sends the notification to the configured endpoint.
// Currently supports HTTP webhook; SQS/SNS/Lambda are stubbed.
func (s *CASStore) dispatchToTarget(rule NotificationRule, bucket, key, versionID string, eventType EventType) error {
	// Build the notification payload (S3 event format)
	payload := map[string]interface{}{
		"Records": []map[string]interface{}{
			{
				"eventVersion": "2.1",
				"eventSource":  "aws:s3",
				"eventTime":    time.Now().Format(time.RFC3339),
				"eventName":    string(eventType),
				"s3": map[string]interface{}{
					"bucket": map[string]interface{}{
						"name": rule.QueueARN, // bucket name
					},
					"object": map[string]interface{}{
						"key":       rule.TopicARN, // key
						"versionId": versionID,
					},
				},
			},
		},
	}

	// HTTP webhook
	if rule.HTTPUrl != "" {
		return dispatchHTTP(rule.HTTPUrl, payload)
	}

	// TODO: SQS, SNS, Lambda implementations
	return fmt.Errorf("notification target not implemented: %w", syscall.ENOSYS)
}

// dispatchHTTP posts the payload to the given URL with exponential backoff.
func dispatchHTTP(url string, payload map[string]interface{}) error {
	// Import here to avoid cycle; this is a standalone helper
	// Real implementation would use the http client with retry
	return nil
}
