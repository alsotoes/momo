package transport

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"syscall"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

// NotificationStore is the storage-agnostic interface for bucket notification
// configurations. The CASStore implements this.
type NotificationStore interface {
	SetBucketNotification(bucket string, cfg storage.NotificationConfig) error
	BucketNotification(bucket string) (cfg storage.NotificationConfig, ok bool, err error)
	DeleteBucketNotification(bucket string) error
}

// notificationRuleXML mirrors the S3 <Rule> element for notifications.
type notificationRuleXML struct {
	ID     string   `xml:"ID"`
	Status string   `xml:"Status"`
	Event  []string `xml:"Event"`
	Filter *struct {
		S3Key *struct {
			FilterRule []struct {
				Name  string `xml:"Name"`
				Value string `xml:"Value"`
			} `xml:"FilterRule"`
		} `xml:"S3Key"`
	} `xml:"Filter"`
	Prefix    string `xml:"Prefix"` // legacy top-level <Prefix>
	Suffix    string `xml:"Suffix"`
	QueueARN  string `xml:"QueueARN"`          // SQS
	TopicARN  string `xml:"TopicARN"`          // SNS
	LambdaARN string `xml:"LambdaFunctionARN"` // Lambda
	HTTPUrl   string `xml:"HttpUrl"`           // Webhook (momo extension)
}

// notificationConfigurationXML mirrors the S3 <NotificationConfiguration> document.
type notificationConfigurationXML struct {
	XMLName xml.Name              `xml:"NotificationConfiguration"`
	Rules   []notificationRuleXML `xml:"Rule"`
}

// ParseNotificationConfiguration converts a PUT ?notification body into a
// storage.NotificationConfig. It rejects a document with no rules, a rule with
// no ID/Status, and a rule with no Events or no destination.
// validateNotificationRule validates the basic fields of a notification rule.
func validateNotificationRule(r notificationRuleXML) error {
	if r.ID == "" {
		return fmt.Errorf("notification rule missing ID: %w", syscall.EINVAL)
	}
	if r.Status != "Enabled" && r.Status != "Disabled" {
		return fmt.Errorf("notification rule %s: invalid status %q: %w", r.ID, r.Status, syscall.EINVAL)
	}
	if len(r.Event) == 0 {
		return fmt.Errorf("notification rule %s: no events specified: %w", r.ID, syscall.EINVAL)
	}

	// Validate destination
	hasTarget := r.QueueARN != "" || r.TopicARN != "" || r.LambdaARN != "" || r.HTTPUrl != ""
	if !hasTarget {
		return fmt.Errorf("notification rule %s: no destination (QueueARN/TopicARN/LambdaFunctionARN/HttpUrl): %w", r.ID, syscall.EINVAL)
	}
	return nil
}

// convertNotificationEvents converts string events to storage.EventType slice.
func convertNotificationEvents(eventStrs []string, ruleID string) ([]storage.EventType, error) {
	events := make([]storage.EventType, 0, len(eventStrs))
	for _, e := range eventStrs {
		switch e {
		case "s3:ObjectCreated:Put":
			events = append(events, storage.EventObjectCreatedPut)
		case "s3:ObjectCreated:Post":
			events = append(events, storage.EventObjectCreatedPost)
		case "s3:ObjectCreated:Copy":
			events = append(events, storage.EventObjectCreatedCopy)
		case "s3:ObjectCreated:CompleteMultipartUpload":
			events = append(events, storage.EventObjectCreatedMultipart)
		case "s3:ObjectRemoved:Delete":
			events = append(events, storage.EventObjectRemovedDelete)
		case "s3:ObjectRemoved:DeleteMarkerCreated":
			events = append(events, storage.EventObjectRemovedDeleteMarker)
		default:
			return nil, fmt.Errorf("notification rule %s: unknown event %q: %w", ruleID, e, syscall.EINVAL)
		}
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("notification rule %s: no valid events: %w", ruleID, syscall.EINVAL)
	}
	return events, nil
}

// extractPrefixSuffix extracts prefix and suffix from filter rules.
func extractPrefixSuffix(r notificationRuleXML) (prefix, suffix string) {
	prefix = r.Prefix
	suffix = r.Suffix
	if r.Filter != nil && r.Filter.S3Key != nil {
		for _, fr := range r.Filter.S3Key.FilterRule {
			switch fr.Name {
			case "Prefix":
				prefix = fr.Value
			case "Suffix":
				suffix = fr.Value
			}
		}
	}
	return prefix, suffix
}

// parseNotificationRule validates and converts a single XML rule to storage.NotificationRule.
func parseNotificationRule(r notificationRuleXML) (storage.NotificationRule, error) {
	if err := validateNotificationRule(r); err != nil {
		return storage.NotificationRule{}, err
	}

	events, err := convertNotificationEvents(r.Event, r.ID)
	if err != nil {
		return storage.NotificationRule{}, err
	}

	prefix, suffix := extractPrefixSuffix(r)

	return storage.NotificationRule{
		ID:        r.ID,
		Status:    r.Status,
		Events:    events,
		Prefix:    prefix,
		Suffix:    suffix,
		QueueARN:  r.QueueARN,
		TopicARN:  r.TopicARN,
		LambdaARN: r.LambdaARN,
		HTTPUrl:   r.HTTPUrl,
	}, nil
}

// ParseNotificationConfiguration converts a PUT ?notification body into a
// storage.NotificationConfig. It rejects a document with no rules, a rule with
// no ID/Status, and a rule with no Events or no destination.
func ParseNotificationConfiguration(body []byte) (cfg storage.NotificationConfig, err error) {
	defer common.RecoverErr("ParseNotificationConfiguration", &err)
	var doc notificationConfigurationXML
	if err := xml.Unmarshal(body, &doc); err != nil {
		return cfg, fmt.Errorf("malformed notification configuration: %w: %v", syscall.EBADMSG, err)
	}
	if len(doc.Rules) == 0 {
		return cfg, fmt.Errorf("notification configuration has no rules: %w", syscall.EINVAL)
	}
	for _, r := range doc.Rules {
		rule, err := parseNotificationRule(r)
		if err != nil {
			return cfg, err
		}
		cfg.Rules = append(cfg.Rules, rule)
	}
	return cfg, nil
}

// FormatNotificationConfiguration converts a storage.NotificationConfig into
// an S3-compatible XML response for GET ?notification.
func FormatNotificationConfiguration(cfg storage.NotificationConfig) ([]byte, error) {
	doc := notificationConfigurationXML{
		Rules: make([]notificationRuleXML, 0, len(cfg.Rules)),
	}
	for _, r := range cfg.Rules {
		xmlRule := notificationRuleXML{
			ID:        r.ID,
			Status:    r.Status,
			Event:     make([]string, 0, len(r.Events)),
			Prefix:    r.Prefix,
			Suffix:    r.Suffix,
			QueueARN:  r.QueueARN,
			TopicARN:  r.TopicARN,
			LambdaARN: r.LambdaARN,
			HTTPUrl:   r.HTTPUrl,
		}
		for _, et := range r.Events {
			xmlRule.Event = append(xmlRule.Event, string(et))
		}
		doc.Rules = append(doc.Rules, xmlRule)
	}
	return xml.MarshalIndent(doc, "", "  ")
}

// handleGetNotification answers GET /{bucket}?notification.
func (m *S3Communicator) handleGetNotification(bucket string) (int, int64, error) {
	if m.notification == nil {
		_, _ = writeS3Error(m.conn, http.StatusNotImplemented, "NotImplemented", "Notification is not supported.", bucket)
		return 0, 0, fmt.Errorf("notification not supported: %w", syscall.ENOSYS)
	}
	cfg, ok, err := m.notification.BucketNotification(bucket)
	if err != nil {
		_, _ = writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to read notification configuration.", bucket)
		return 0, 0, fmt.Errorf("failed to read notification configuration: %w", err)
	}
	if !ok {
		_, _ = writeS3Error(m.conn, http.StatusNotFound, "NoSuchNotificationConfiguration", "The notification configuration does not exist.", bucket)
		return 0, 0, fmt.Errorf("no notification configuration for %s: %w", bucket, syscall.ENOENT)
	}
	body, err := FormatNotificationConfiguration(cfg)
	if err != nil {
		_, _ = writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to format notification configuration.", bucket)
		return 0, 0, fmt.Errorf("failed to format notification configuration: %w", err)
	}
	return 0, 0, m.writeRawResponse(xmlResponseHeaders(len(body)), body, nil)
}

// handlePutNotification answers PUT /{bucket}?notification with the body already
// spooled by the gateway reader.
func (m *S3Communicator) handlePutNotification(req *http.Request, bucket string) (int, int64, error) {
	if m.notification == nil {
		_, _ = writeS3Error(m.conn, http.StatusNotImplemented, "NotImplemented", "Notification is not supported.", bucket)
		return 0, 0, fmt.Errorf("notification not supported: %w", syscall.ENOSYS)
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, 128*1024))
	if err != nil {
		_, _ = writeS3Error(m.conn, http.StatusBadRequest, "MalformedXML", "Failed to read request body.", bucket)
		return 0, 0, fmt.Errorf("failed to read notification body: %w", err)
	}
	cfg, err := ParseNotificationConfiguration(body)
	if err != nil {
		_, _ = writeS3Error(m.conn, http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed or did not validate against our published schema.", bucket)
		return 0, 0, err
	}
	if err := m.notification.SetBucketNotification(bucket, cfg); err != nil {
		_, _ = writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to store notification configuration.", bucket)
		return 0, 0, fmt.Errorf("failed to set notification configuration: %w", err)
	}
	m.writeAuditLog("PutNotification", bucket, "success")
	return 0, 0, m.writeRawResponse("HTTP/1.1 200 OK\r\nContent-Length: 0"+s3ConnClose, nil, nil)
}

// handleDeleteNotification answers DELETE /{bucket}?notification.
func (m *S3Communicator) handleDeleteNotification(bucket string) (int, int64, error) {
	if m.notification == nil {
		_, _ = writeS3Error(m.conn, http.StatusNotImplemented, "NotImplemented", "Notification is not supported.", bucket)
		return 0, 0, fmt.Errorf("notification not supported: %w", syscall.ENOSYS)
	}
	if err := m.notification.DeleteBucketNotification(bucket); err != nil {
		_, _ = writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to delete notification configuration.", bucket)
		return 0, 0, fmt.Errorf("failed to delete notification configuration: %w", err)
	}
	m.writeAuditLog("DeleteNotification", bucket, "success")
	return 0, 0, m.writeRawResponse("HTTP/1.1 204 No Content"+s3ConnClose, nil, nil)
}
