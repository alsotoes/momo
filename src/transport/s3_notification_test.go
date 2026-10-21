package transport

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// mockDispatchStore records gateway-dispatched events.
type mockDispatchStore struct {
	mockNotificationStore
	keys  []string
	retry int
}

func (m *mockDispatchStore) DispatchNotification(bucket, key, versionID string, et storage.EventType, now time.Time, retryMax int) (storage.NotificationOutcome, error) {
	m.keys = append(m.keys, bucket+"/"+key)
	m.retry = retryMax
	return storage.NotificationOutcome{Dispatched: 1}, nil
}

// handlerPipe builds an S3Communicator writing to a pipe and returns a function
// that closes the write side and yields everything the handler wrote.
func handlerPipe(t *testing.T) (*S3Communicator, func() string) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close(); _ = serverConn.Close() })
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(&buf, clientConn)
	}()
	m := NewS3Communicator(serverConn)
	return m, func() string {
		_ = serverConn.Close()
		<-done
		return buf.String()
	}
}

func TestHandleGetNotification(t *testing.T) {
	t.Run("unsupported store returns 501", func(t *testing.T) {
		m, read := handlerPipe(t)
		if _, _, err := m.handleGetNotification("b"); err == nil {
			t.Fatal("expected error when notification is nil")
		}
		if !strings.Contains(read(), "501") {
			t.Fatal("expected 501 response")
		}
	})

	t.Run("missing config returns 404", func(t *testing.T) {
		m, read := handlerPipe(t)
		m.notification = &mockNotificationStore{}
		if _, _, err := m.handleGetNotification("b"); err == nil {
			t.Fatal("expected error for missing config")
		}
		if !strings.Contains(read(), "404") {
			t.Fatal("expected 404 response")
		}
	})

	t.Run("existing config returns 200 XML", func(t *testing.T) {
		m, read := handlerPipe(t)
		store := &mockNotificationStore{}
		if err := store.SetBucketNotification("b", storage.NotificationConfig{Rules: []storage.NotificationRule{{
			ID: "r1", Status: "Enabled", Events: []storage.EventType{storage.EventObjectCreatedPut},
			HTTPUrl: "http://localhost:9/hook",
		}}}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		m.notification = store
		if _, _, err := m.handleGetNotification("b"); err != nil {
			t.Fatalf("handleGetNotification: %v", err)
		}
		resp := read()
		if !strings.Contains(resp, "200 OK") || !strings.Contains(resp, "<NotificationConfiguration") {
			t.Fatalf("unexpected response: %q", resp)
		}
	})
}

func TestHandlePutNotification(t *testing.T) {
	t.Run("unsupported store returns 501", func(t *testing.T) {
		m, read := handlerPipe(t)
		req := httptest.NewRequest("PUT", "/b?notification", strings.NewReader("<x/>"))
		if _, _, err := m.handlePutNotification(req, "b"); err == nil {
			t.Fatal("expected error when notification is nil")
		}
		if !strings.Contains(read(), "501") {
			t.Fatal("expected 501 response")
		}
	})

	t.Run("malformed body returns 400", func(t *testing.T) {
		m, read := handlerPipe(t)
		m.notification = &mockNotificationStore{}
		req := httptest.NewRequest("PUT", "/b?notification", strings.NewReader("<NotificationConfiguration></NotificationConfiguration>"))
		if _, _, err := m.handlePutNotification(req, "b"); err == nil {
			t.Fatal("expected error for empty rules")
		}
		if !strings.Contains(read(), "400") {
			t.Fatal("expected 400 response")
		}
	})

	t.Run("valid body stores and returns 200", func(t *testing.T) {
		m, read := handlerPipe(t)
		store := &mockNotificationStore{}
		m.notification = store
		body := `<NotificationConfiguration><Rule><ID>r1</ID><Status>Enabled</Status><Event>s3:ObjectCreated:Put</Event><QueueARN>arn:aws:sqs:us-east-1:1:q</QueueARN></Rule></NotificationConfiguration>`
		req := httptest.NewRequest("PUT", "/b?notification", strings.NewReader(body))
		if _, _, err := m.handlePutNotification(req, "b"); err != nil {
			t.Fatalf("handlePutNotification: %v", err)
		}
		if !strings.Contains(read(), "200 OK") {
			t.Fatal("expected 200 response")
		}
		cfg, ok, _ := store.BucketNotification("b")
		if !ok || len(cfg.Rules) != 1 {
			t.Fatalf("config was not stored: ok=%v cfg=%+v", ok, cfg)
		}
	})
}

func TestHandleDeleteNotification(t *testing.T) {
	t.Run("unsupported store returns 501", func(t *testing.T) {
		m, read := handlerPipe(t)
		if _, _, err := m.handleDeleteNotification("b"); err == nil {
			t.Fatal("expected error when notification is nil")
		}
		if !strings.Contains(read(), "501") {
			t.Fatal("expected 501 response")
		}
	})

	t.Run("success returns 204", func(t *testing.T) {
		m, read := handlerPipe(t)
		store := &mockNotificationStore{}
		_ = store.SetBucketNotification("b", storage.NotificationConfig{})
		m.notification = store
		if _, _, err := m.handleDeleteNotification("b"); err != nil {
			t.Fatalf("handleDeleteNotification: %v", err)
		}
		if !strings.Contains(read(), "204") {
			t.Fatal("expected 204 response")
		}
		if _, ok, _ := store.BucketNotification("b"); ok {
			t.Fatal("config should be deleted")
		}
	})
}

func TestDispatchNotificationHelper(t *testing.T) {
	store := &mockDispatchStore{}
	m := &S3Communicator{notification: store, notificationRetryMax: 7}
	m.dispatchNotification("bucket", "key", "v1", storage.EventObjectCreatedMultipart)
	if len(store.keys) != 1 || store.keys[0] != "bucket/key" {
		t.Fatalf("expected event dispatched, got %v", store.keys)
	}
	if store.retry != 7 {
		t.Fatalf("expected configured retry max 7, got %d", store.retry)
	}

	// Unset retry max falls back to the package default.
	store2 := &mockDispatchStore{}
	m2 := &S3Communicator{notification: store2}
	m2.dispatchNotification("bucket", "key", "", storage.EventObjectRemovedDelete)
	if store2.retry != defaultNotificationRetryMax {
		t.Fatalf("expected default retry max %d, got %d", defaultNotificationRetryMax, store2.retry)
	}

	// Nil notification capability is inert.
	m3 := &S3Communicator{}
	m3.dispatchNotification("bucket", "key", "", storage.EventObjectCreatedPut)
}

func TestConvertNotificationEvents(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		want    []storage.EventType
		wantErr bool
	}{
		{"put", []string{"s3:ObjectCreated:Put"}, []storage.EventType{storage.EventObjectCreatedPut}, false},
		{"post", []string{"s3:ObjectCreated:Post"}, []storage.EventType{storage.EventObjectCreatedPost}, false},
		{"copy", []string{"s3:ObjectCreated:Copy"}, []storage.EventType{storage.EventObjectCreatedCopy}, false},
		{"multipart", []string{"s3:ObjectCreated:CompleteMultipartUpload"}, []storage.EventType{storage.EventObjectCreatedMultipart}, false},
		{"delete", []string{"s3:ObjectRemoved:Delete"}, []storage.EventType{storage.EventObjectRemovedDelete}, false},
		{"delete-marker", []string{"s3:ObjectRemoved:DeleteMarkerCreated"}, []storage.EventType{storage.EventObjectRemovedDeleteMarker}, false},
		{"mixed", []string{"s3:ObjectCreated:Put", "s3:ObjectRemoved:Delete"}, []storage.EventType{storage.EventObjectCreatedPut, storage.EventObjectRemovedDelete}, false},
		{"unknown", []string{"s3:NoSuch:Event"}, nil, true},
		{"empty", nil, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := convertNotificationEvents(tc.in, "r1")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertEventTypes(t, got, tc.want)
		})
	}
}

// assertEventTypes compares two event-type slices element-wise.
func assertEventTypes(t *testing.T, got, want []storage.EventType) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestExtractPrefixSuffixLegacy(t *testing.T) {
	// Legacy top-level <Prefix>/<Suffix> are used when no Filter is present.
	p, s := extractPrefixSuffix(notificationRuleXML{Prefix: "legacy/", Suffix: ".old"})
	if p != "legacy/" || s != ".old" {
		t.Fatalf("got prefix=%q suffix=%q", p, s)
	}
	// Filter rules override the legacy fields.
	rule := notificationRuleXML{
		Prefix: "legacy/", Suffix: ".old",
		Filter: &notificationFilterXML{S3Key: &notificationS3KeyXML{FilterRule: []notificationFilterRuleXML{
			{Name: "Prefix", Value: "filtered/"},
			{Name: "Suffix", Value: ".new"},
		}}},
	}
	p, s = extractPrefixSuffix(rule)
	if p != "filtered/" || s != ".new" {
		t.Fatalf("filter should win: got prefix=%q suffix=%q", p, s)
	}
}

// errNotificationStore injects errors into each NotificationStore method.
type errNotificationStore struct {
	mockNotificationStore
	getErr, setErr, delErr error
}

func (m *errNotificationStore) BucketNotification(b string) (storage.NotificationConfig, bool, error) {
	if m.getErr != nil {
		return storage.NotificationConfig{}, false, m.getErr
	}
	return m.mockNotificationStore.BucketNotification(b)
}

func (m *errNotificationStore) SetBucketNotification(b string, c storage.NotificationConfig) error {
	if m.setErr != nil {
		return m.setErr
	}
	return m.mockNotificationStore.SetBucketNotification(b, c)
}

func (m *errNotificationStore) DeleteBucketNotification(b string) error {
	if m.delErr != nil {
		return m.delErr
	}
	return m.mockNotificationStore.DeleteBucketNotification(b)
}

// assertHandlerError runs call and asserts it errors with the wanted status code.
func assertHandlerError(t *testing.T, read func() string, call func() error, wantStatus string) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(read(), wantStatus) {
		t.Fatalf("expected %s response", wantStatus)
	}
}

func TestNotificationHandlerErrorPaths(t *testing.T) {
	boom := errors.New("boom")

	t.Run("get read error returns 500", func(t *testing.T) {
		m, read := handlerPipe(t)
		m.notification = &errNotificationStore{getErr: boom}
		assertHandlerError(t, read, func() error { _, _, err := m.handleGetNotification("b"); return err }, "500")
	})

	t.Run("put store error returns 500", func(t *testing.T) {
		m, read := handlerPipe(t)
		m.notification = &errNotificationStore{setErr: boom}
		body := `<NotificationConfiguration><Rule><ID>r1</ID><Status>Enabled</Status><Event>s3:ObjectCreated:Put</Event><QueueARN>arn:aws:sqs:us-east-1:1:q</QueueARN></Rule></NotificationConfiguration>`
		req := httptest.NewRequest("PUT", "/b?notification", strings.NewReader(body))
		assertHandlerError(t, read, func() error { _, _, err := m.handlePutNotification(req, "b"); return err }, "500")
	})

	t.Run("put body read error returns 400", func(t *testing.T) {
		m, read := handlerPipe(t)
		m.notification = &mockNotificationStore{}
		req := httptest.NewRequest("PUT", "/b?notification", errReader{})
		assertHandlerError(t, read, func() error { _, _, err := m.handlePutNotification(req, "b"); return err }, "400")
	})

	t.Run("delete store error returns 500", func(t *testing.T) {
		m, read := handlerPipe(t)
		m.notification = &errNotificationStore{delErr: boom}
		assertHandlerError(t, read, func() error { _, _, err := m.handleDeleteNotification("b"); return err }, "500")
	})
}

// errReader always fails on Read.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (errReader) Close() error             { return nil }

func TestSetNotificationRetryMax(t *testing.T) {
	m := &S3Communicator{}
	m.SetNotificationRetryMax(5)
	if m.notificationRetryMax != 5 {
		t.Fatalf("expected 5, got %d", m.notificationRetryMax)
	}
	// Values < 1 are ignored.
	m.SetNotificationRetryMax(0)
	if m.notificationRetryMax != 5 {
		t.Fatalf("expected 0 to be ignored, got %d", m.notificationRetryMax)
	}
}
