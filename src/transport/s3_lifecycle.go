package transport

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"syscall"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

// LifecycleStore is the capability interface the S3 gateway needs for R10
// lifecycle configuration (implemented by *storage.CASStore). Declared here per
// the consumer-side interface pattern so the communicator never depends on the
// concrete store.
type LifecycleStore interface {
	SetBucketLifecycle(bucket string, cfg storage.LifecycleConfig) error
	BucketLifecycle(bucket string) (cfg storage.LifecycleConfig, ok bool, err error)
	DeleteBucketLifecycle(bucket string) error
}

// lifecycleRuleXML mirrors the S3 <Rule> element.
type lifecycleRuleXML struct {
	ID     string `xml:"ID"`
	Status string `xml:"Status"`
	Filter *struct {
		Prefix string `xml:"Prefix"`
	} `xml:"Filter"`
	Prefix     string `xml:"Prefix"` // legacy top-level <Prefix>
	Expiration *struct {
		Days int `xml:"Days"`
	} `xml:"Expiration"`
	Transition *struct {
		Days         int    `xml:"Days"`
		StorageClass string `xml:"StorageClass"`
	} `xml:"Transition"`
}

// lifecycleConfigurationXML mirrors the S3 <LifecycleConfiguration> document.
type lifecycleConfigurationXML struct {
	XMLName xml.Name           `xml:"LifecycleConfiguration"`
	Rules   []lifecycleRuleXML `xml:"Rule"`
}

// ParseLifecycleConfiguration converts a PUT ?lifecycle body into a
// storage.LifecycleConfig. It rejects a document with no rules, a rule with no
// ID/Status, and a rule with neither an Expiration nor a Transition action.
func ParseLifecycleConfiguration(body []byte) (cfg storage.LifecycleConfig, err error) {
	defer common.RecoverErr("ParseLifecycleConfiguration", &err)
	var doc lifecycleConfigurationXML
	if err := xml.Unmarshal(body, &doc); err != nil {
		return cfg, fmt.Errorf("malformed lifecycle configuration: %w: %v", syscall.EBADMSG, err)
	}
	if len(doc.Rules) == 0 {
		return cfg, fmt.Errorf("lifecycle configuration has no rules: %w", syscall.EINVAL)
	}
	for _, r := range doc.Rules {
		rule, err := convertLifecycleRule(r)
		if err != nil {
			return cfg, err
		}
		cfg.Rules = append(cfg.Rules, rule)
	}
	return cfg, nil
}

// convertLifecycleRule validates and converts one XML rule.
func convertLifecycleRule(r lifecycleRuleXML) (storage.LifecycleRule, error) {
	status := strings.TrimSpace(r.Status)
	if status == "" {
		return storage.LifecycleRule{}, fmt.Errorf("lifecycle rule missing Status: %w", syscall.EINVAL)
	}
	if status != storage.LifecycleEnabled && status != storage.LifecycleDisabled {
		return storage.LifecycleRule{}, fmt.Errorf("lifecycle rule has invalid Status %q: %w", status, syscall.EINVAL)
	}
	rule := storage.LifecycleRule{
		ID:     strings.TrimSpace(r.ID),
		Status: status,
		Prefix: r.Prefix,
	}
	if r.Filter != nil {
		rule.Prefix = r.Filter.Prefix
	}
	if r.Expiration != nil {
		if r.Expiration.Days <= 0 {
			return storage.LifecycleRule{}, fmt.Errorf("lifecycle Expiration requires Days > 0: %w", syscall.EINVAL)
		}
		rule.ExpirationDays = r.Expiration.Days
	}
	if r.Transition != nil {
		if r.Transition.Days <= 0 || strings.TrimSpace(r.Transition.StorageClass) == "" {
			return storage.LifecycleRule{}, fmt.Errorf("lifecycle Transition requires Days > 0 and StorageClass: %w", syscall.EINVAL)
		}
		rule.TransitionDays = r.Transition.Days
		rule.StorageClass = strings.TrimSpace(r.Transition.StorageClass)
	}
	if rule.ExpirationDays == 0 && rule.TransitionDays == 0 {
		return storage.LifecycleRule{}, fmt.Errorf("lifecycle rule has no Expiration or Transition action: %w", syscall.EINVAL)
	}
	return rule, nil
}

// FormatLifecycleConfigurationXML renders the GET ?lifecycle response body.
func FormatLifecycleConfigurationXML(cfg storage.LifecycleConfig) []byte {
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString(`<LifecycleConfiguration xmlns="` + s3XMLNS + `">`)
	for _, r := range cfg.Rules {
		b.WriteString("<Rule><ID>")
		xmlEscape(&b, r.ID)
		b.WriteString("</ID><Status>")
		xmlEscape(&b, r.Status)
		b.WriteString("</Status>")
		if r.Prefix != "" {
			b.WriteString("<Filter><Prefix>")
			xmlEscape(&b, r.Prefix)
			b.WriteString("</Prefix></Filter>")
		}
		if r.ExpirationDays > 0 {
			fmt.Fprintf(&b, "<Expiration><Days>%d</Days></Expiration>", r.ExpirationDays)
		}
		if r.TransitionDays > 0 {
			fmt.Fprintf(&b, "<Transition><Days>%d</Days><StorageClass>", r.TransitionDays)
			xmlEscape(&b, r.StorageClass)
			b.WriteString("</StorageClass></Transition>")
		}
		b.WriteString("</Rule>")
	}
	b.WriteString("</LifecycleConfiguration>")
	return b.Bytes()
}

// handleGetLifecycle answers GET /{bucket}?lifecycle.
func (m *S3Communicator) handleGetLifecycle(bucket string) (int, int64, error) {
	cfg, ok, err := m.lifecycle.BucketLifecycle(bucket)
	if err != nil {
		_, _ = writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to read lifecycle configuration.", bucket)
		return 0, 0, fmt.Errorf("failed to read lifecycle configuration: %w", err)
	}
	if !ok {
		_, _ = writeS3Error(m.conn, http.StatusNotFound, "NoSuchLifecycleConfiguration", "The lifecycle configuration does not exist.", bucket)
		return 0, 0, fmt.Errorf("no lifecycle configuration for %s: %w", bucket, syscall.ENOENT)
	}
	body := FormatLifecycleConfigurationXML(cfg)
	return 0, 0, m.writeRawResponse(xmlResponseHeaders(len(body)), body, nil)
}

// handlePutLifecycle answers PUT /{bucket}?lifecycle with the body already
// spooled by the gateway reader.
func (m *S3Communicator) handlePutLifecycle(req *http.Request, bucket string) (int, int64, error) {
	body, err := io.ReadAll(io.LimitReader(req.Body, 128*1024))
	if err != nil {
		_, _ = writeS3Error(m.conn, http.StatusBadRequest, "MalformedXML", "Failed to read request body.", bucket)
		return 0, 0, fmt.Errorf("failed to read lifecycle body: %w", err)
	}
	cfg, err := ParseLifecycleConfiguration(body)
	if err != nil {
		_, _ = writeS3Error(m.conn, http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed or did not validate against our published schema.", bucket)
		return 0, 0, err
	}
	if err := m.lifecycle.SetBucketLifecycle(bucket, cfg); err != nil {
		_, _ = writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to store lifecycle configuration.", bucket)
		return 0, 0, fmt.Errorf("failed to set lifecycle configuration: %w", err)
	}
	m.writeAuditLog("PutLifecycle", bucket, "success")
	return 0, 0, m.writeRawResponse("HTTP/1.1 200 OK\r\nContent-Length: 0"+s3ConnClose, nil, nil)
}

// handleDeleteLifecycle answers DELETE /{bucket}?lifecycle.
func (m *S3Communicator) handleDeleteLifecycle(bucket string) (int, int64, error) {
	if err := m.lifecycle.DeleteBucketLifecycle(bucket); err != nil {
		_, _ = writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to delete lifecycle configuration.", bucket)
		return 0, 0, fmt.Errorf("failed to delete lifecycle configuration: %w", err)
	}
	m.writeAuditLog("DeleteLifecycle", bucket, "success")
	return 0, 0, m.writeRawResponse("HTTP/1.1 204 No Content"+s3ConnClose, nil, nil)
}
