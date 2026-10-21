package transport

import (
	"encoding/xml"
	"testing"

	"github.com/alsotoes/momo/src/storage"
)

// mockNotificationStore implements NotificationStore for testing.
type mockNotificationStore struct {
	notifs map[string]storage.NotificationConfig
}

func (m *mockNotificationStore) SetBucketNotification(bucket string, cfg storage.NotificationConfig) error {
	if m.notifs == nil {
		m.notifs = make(map[string]storage.NotificationConfig)
	}
	m.notifs[bucket] = cfg
	return nil
}

func (m *mockNotificationStore) BucketNotification(bucket string) (storage.NotificationConfig, bool, error) {
	cfg, ok := m.notifs[bucket]
	return cfg, ok, nil
}

func (m *mockNotificationStore) DeleteBucketNotification(bucket string) error {
	if m.notifs != nil {
		delete(m.notifs, bucket)
	}
	return nil
}

func TestParseNotificationConfiguration(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<NotificationConfiguration>
  <Rule>
    <ID>rule-1</ID>
    <Status>Enabled</Status>
    <Event>s3:ObjectCreated:Put</Event>
    <Event>s3:ObjectCreated:Post</Event>
    <Filter>
      <S3Key>
        <FilterRule>
          <Name>Prefix</Name>
          <Value>logs/</Value>
        </FilterRule>
      </S3Key>
    </Filter>
    <QueueARN>arn:aws:sqs:us-east-1:123456789012:my-queue</QueueARN>
  </Rule>
  <Rule>
    <ID>rule-2</ID>
    <Status>Enabled</Status>
    <Event>s3:ObjectRemoved:Delete</Event>
    <Suffix>.log</Suffix>
    <TopicARN>arn:aws:sns:us-east-1:123456789012:my-topic</TopicARN>
  </Rule>
</NotificationConfiguration>`)

	cfg, err := ParseNotificationConfiguration(body)
	if err != nil {
		t.Fatalf("ParseNotificationConfiguration: %v", err)
	}
	if len(cfg.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(cfg.Rules))
	}
	if cfg.Rules[0].ID != "rule-1" {
		t.Fatalf("expected rule-1, got %s", cfg.Rules[0].ID)
	}
	if len(cfg.Rules[0].Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(cfg.Rules[0].Events))
	}
	if cfg.Rules[0].Prefix != "logs/" {
		t.Fatalf("expected prefix logs/, got %s", cfg.Rules[0].Prefix)
	}
	if cfg.Rules[1].Suffix != ".log" {
		t.Fatalf("expected suffix .log, got %s", cfg.Rules[1].Suffix)
	}
}

func TestParseNotificationConfigurationInvalid(t *testing.T) {
	// No rules
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<NotificationConfiguration></NotificationConfiguration>`)
	_, err := ParseNotificationConfiguration(body)
	if err == nil {
		t.Fatal("expected error for empty rules")
	}

	// No ID
	body = []byte(`<?xml version="1.0" encoding="UTF-8"?>
<NotificationConfiguration>
  <Rule>
    <Status>Enabled</Status>
    <Event>s3:ObjectCreated:Put</Event>
    <QueueARN>arn:aws:sqs:us-east-1:123456789012:my-queue</QueueARN>
  </Rule>
</NotificationConfiguration>`)
	_, err = ParseNotificationConfiguration(body)
	if err == nil {
		t.Fatal("expected error for missing ID")
	}

	// No events
	body = []byte(`<?xml version="1.0" encoding="UTF-8"?>
<NotificationConfiguration>
  <Rule>
    <ID>rule-1</ID>
    <Status>Enabled</Status>
    <QueueARN>arn:aws:sqs:us-east-1:123456789012:my-queue</QueueARN>
  </Rule>
</NotificationConfiguration>`)
	_, err = ParseNotificationConfiguration(body)
	if err == nil {
		t.Fatal("expected error for no events")
	}

	// No destination
	body = []byte(`<?xml version="1.0" encoding="UTF-8"?>
<NotificationConfiguration>
  <Rule>
    <ID>rule-1</ID>
    <Status>Enabled</Status>
    <Event>s3:ObjectCreated:Put</Event>
  </Rule>
</NotificationConfiguration>`)
	_, err = ParseNotificationConfiguration(body)
	if err == nil {
		t.Fatal("expected error for no destination")
	}

	// Invalid status
	body = []byte(`<?xml version="1.0" encoding="UTF-8"?>
<NotificationConfiguration>
  <Rule>
    <ID>rule-1</ID>
    <Status>Invalid</Status>
    <Event>s3:ObjectCreated:Put</Event>
    <QueueARN>arn:aws:sqs:us-east-1:123456789012:my-queue</QueueARN>
  </Rule>
</NotificationConfiguration>`)
	_, err = ParseNotificationConfiguration(body)
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestFormatNotificationConfiguration(t *testing.T) {
	cfg := storage.NotificationConfig{
		Rules: []storage.NotificationRule{
			{
				ID:       "rule-1",
				Status:   "Enabled",
				Events:   []storage.EventType{storage.EventObjectCreatedPut, storage.EventObjectCreatedPost},
				Prefix:   "logs/",
				QueueARN: "arn:aws:sqs:us-east-1:123456789012:my-queue",
			},
			{
				ID:       "rule-2",
				Status:   "Enabled",
				Events:   []storage.EventType{storage.EventObjectRemovedDelete},
				Suffix:   ".log",
				TopicARN: "arn:aws:sns:us-east-1:123456789012:my-topic",
			},
		},
	}

	body, err := FormatNotificationConfiguration(cfg)
	if err != nil {
		t.Fatalf("FormatNotificationConfiguration: %v", err)
	}

	// Parse it back
	var doc notificationConfigurationXML
	if err := xml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	if len(doc.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(doc.Rules))
	}
	if doc.Rules[0].ID != "rule-1" {
		t.Fatalf("expected rule-1, got %s", doc.Rules[0].ID)
	}
	if doc.Rules[0].Prefix != "logs/" {
		t.Fatalf("expected prefix logs/, got %s", doc.Rules[0].Prefix)
	}
	if doc.Rules[1].Suffix != ".log" {
		t.Fatalf("expected suffix .log, got %s", doc.Rules[1].Suffix)
	}
}

func TestNotificationStore(t *testing.T) {
	store := &mockNotificationStore{}

	// Test SetBucketNotification
	cfg := storage.NotificationConfig{
		Rules: []storage.NotificationRule{
			{ID: "rule-1", Status: "Enabled", Events: []storage.EventType{storage.EventObjectCreatedPut}, Prefix: "logs/", HTTPUrl: "http://localhost:9000/webhook"},
		},
	}
	if err := store.SetBucketNotification("test-bucket", cfg); err != nil {
		t.Fatalf("SetBucketNotification: %v", err)
	}

	// Test BucketNotification
	got, ok, err := store.BucketNotification("test-bucket")
	if err != nil {
		t.Fatalf("BucketNotification error: %v", err)
	}
	if !ok {
		t.Fatal("expected config to exist")
	}
	if len(got.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(got.Rules))
	}

	// Test DeleteBucketNotification
	if err := store.DeleteBucketNotification("test-bucket"); err != nil {
		t.Fatalf("DeleteBucketNotification: %v", err)
	}
	_, ok2, _ := store.BucketNotification("test-bucket")
	if ok2 {
		t.Fatal("expected config to not exist after delete")
	}
}
