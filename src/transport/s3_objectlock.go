package transport

// S3 Object Lock surface (R10, issue #938). Scope agreed with alsotoes:
//   - Object lock is enabled only at bucket creation (CreateBucket +
//     x-amz-bucket-object-lock-enabled); disabling is refused (409).
//   - Plain-PUT lock capture is a documented ceiling: the gateway response
//     precedes body streaming, so x-amz-object-lock-* on single PUTs cannot
//     reach the version record this phase — lock via multipart uploads.
//   - ?retention / ?legal-hold subresource APIs are implemented (Phase 5).

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"io"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ObjectLockStore is the capability seam for object-lock state.
type ObjectLockStore interface {
	SetBucketObjectLock(bucket string) error
	BucketObjectLockEnabled(bucket string) (bool, error)
	PutVersionLock(bucket, key, versionID string, state storage.ObjectLockState, bypassGovernance bool) error
	PutVersionRetention(bucket, key, versionID, mode string, retainUntil int64, bypassGovernance bool) error
	PutVersionLegalHold(bucket, key, versionID string, on bool) error
	VersionLock(bucket, key, versionID string) (storage.ObjectLockState, bool, error)
}

// SetObjectLockStore attaches an object-lock capability (nil disables).
func (m *S3Communicator) SetObjectLockStore(lock ObjectLockStore) { m.lock = lock }

// handleGetBucketObjectLock answers GET /{bucket}?object-lock. Unconfigured
// buckets answer 404 NoSuchObjectLockConfiguration per S3.
func (m *S3Communicator) handleGetBucketObjectLock(bucket string) (int, int64, error) {
	enabled, err := m.lock.BucketObjectLockEnabled(bucket)
	if err != nil {
		_, _ = writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to read object lock configuration.", bucket)
		return 0, 0, fmt.Errorf("read object lock status: %w", err)
	}
	if !enabled {
		_, _ = writeS3Error(m.conn, http.StatusNotFound, "NoSuchObjectLockConfiguration", "Object Lock configuration does not exist for this bucket.", bucket)
		return 0, 0, ErrRequestHandled
	}
	body := FormatObjectLockConfigurationXML()
	return 0, 0, m.writeRawResponse(xmlResponseHeaders(len(body)), body, nil)
}

// handlePutBucketObjectLock answers PUT /{bucket}?object-lock. Momo enables
// object lock only at bucket creation: later PUTs are refused with 409
// InvalidBucketState (both enable attempts and disable attempts).
func (m *S3Communicator) handlePutBucketObjectLock(req *http.Request, bucket string) (int, int64, error) {
	body, err := io.ReadAll(io.LimitReader(req.Body, 64*1024))
	if err != nil {
		_, _ = writeS3Error(m.conn, http.StatusBadRequest, "MalformedXML", "Failed to read request body.", bucket)
		return 0, 0, fmt.Errorf("read object lock body: %w", err)
	}
	if err := ParseObjectLockConfiguration(body); err != nil {
		_, _ = writeS3Error(m.conn, http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed or did not validate against our published schema.", bucket)
		return 0, 0, fmt.Errorf("invalid object lock configuration: %w", syscall.EINVAL)
	}
	_, _ = writeS3Error(m.conn, http.StatusConflict, "InvalidBucketState", "Object Lock can only be enabled at bucket creation and cannot be disabled.", bucket)
	m.writeAuditLog("PutObjectLock", bucket, "refused")
	return 0, 0, ErrRequestHandled
}

// ParseObjectLockConfiguration accepts only the bare "Enabled" document.
// ObjectLockRequest rules (default retention) are rejected — the caller maps
// the sentinel to 501 since rule support is out of this phase.
func ParseObjectLockConfiguration(body []byte) error {
	if !bytes.Contains(body, []byte("<ObjectLockEnabled>Enabled</ObjectLockEnabled>")) {
		return syscall.EINVAL
	}
	return nil
}

// FormatObjectLockConfigurationXML renders the enabled-state document (fully
// static XML; no escaping needed).
func FormatObjectLockConfigurationXML() []byte {
	return []byte(`<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectLockEnabled>Enabled</ObjectLockEnabled></ObjectLockConfiguration>`)
}

// injectObjectLockHeaders appends S3 object-lock response headers for a
// version's lock record onto b (GET /{bucket}/{key}?versionId=X and HEAD).
func injectObjectLockHeaders(b *strings.Builder, st storage.ObjectLockState) {
	if st.Mode != "" {
		b.WriteString("x-amz-object-lock-mode: " + st.Mode + "\r\n")
	}
	if st.RetainUntil > 0 {
		b.WriteString("x-amz-object-lock-retain-until-date: " + xmlIso8601(time.Unix(st.RetainUntil, 0).UTC()) + "\r\n")
	}
	if st.LegalHoldOn {
		b.WriteString("x-amz-object-lock-legal-hold-status: ON\r\n")
	}
}

// xmlIso8601 renders time in the S3 timestamp shape (2006-01-02T15:04:05Z).
func xmlIso8601(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

// msgObjectLockUnsupported is the honest-reject body for object-lock routes
// on stores without the lock capability.
const msgObjectLockUnsupported = "Object Lock is not supported."

// rejectObjectLockUnsupported answers 501 NotImplemented and returns the
// connection-teardown error for object-lock routes on stores without the
// object-lock capability.
func (m *S3Communicator) rejectObjectLockUnsupported(resource string) (err error) {
	defer common.RecoverErr("S3 rejectObjectLockUnsupported", &err)
	_ = m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, werr := writeS3Error(m.conn, http.StatusNotImplemented, "NotImplemented", msgObjectLockUnsupported, resource); werr != nil {
		return fmt.Errorf("failed to write 501 response: %v: %w", werr, syscall.ENOTSUP)
	}
	return fmt.Errorf("object lock store unavailable: %w", syscall.ENOTSUP)
}

// LockVersion persists an S3 lock header set for an already-created version
// (multipart completion path, which knows its version id). Content was
// already written, so header problems never damage the object.
func (m *S3Communicator) LockVersion(bucket, key, versionID string, st storage.ObjectLockState) (err error) {
	defer common.RecoverErr("S3Communicator.LockVersion", &err)
	if m.lock == nil {
		if st.Mode != "" || st.RetainUntil > 0 || st.LegalHoldOn {
			_, _ = writeS3Error(m.conn, http.StatusNotImplemented, "NotImplemented", "Object Lock headers were supplied but object lock is not available.", bucket)
			return ErrRequestHandled
		}
		return nil
	}
	if err := m.lock.PutVersionLock(bucket, key, versionID, st, false); err != nil {
		if err == syscall.ENOENT {
			_, _ = writeS3Error(m.conn, http.StatusNotFound, "NoSuchVersion", "The version to lock does not exist.", key)
			return ErrRequestHandled
		}
		if err == syscall.EPERM {
			// Active COMPLIANCE retention may only be extended.
			_, _ = writeS3Error(m.conn, http.StatusForbidden, "AccessDenied", "The requested object lock state change is not permitted.", key)
			return ErrRequestHandled
		}
		if err == syscall.EINVAL {
			_, _ = writeS3Error(m.conn, http.StatusBadRequest, "InvalidArgument", "Invalid object lock header values.", key)
			return ErrRequestHandled
		}
		_, _ = writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to store object lock state.", key)
		return fmt.Errorf("store object lock state: %w", err)
	}
	return nil
}

// multipartLockHeaders parses the object-lock header set for a multipart
// completion and refuses requests the bucket cannot honor (invalid values, or
// lock headers on an unversioned bucket). It writes the S3 error response and
// returns a non-nil error in those cases.
func (m *S3Communicator) multipartLockHeaders(req *http.Request, bucket, key string) (storage.ObjectLockState, bool, error) {
	lockState, lerr := ParseS3LockHeaders(req)
	if lerr != nil {
		_ = m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, _ = writeS3Error(m.conn, http.StatusBadRequest, "InvalidArgument", "Invalid object lock header values.", key)
		return lockState, false, fmt.Errorf("s3 multipart lock headers: %w", lerr)
	}
	requested := lockState.Mode != "" || lockState.RetainUntil > 0 || lockState.LegalHoldOn
	if !requested {
		return lockState, false, nil
	}
	versioned := false
	if m.versioning != nil {
		versioned, _ = m.versioning.IsVersioned(bucket)
	}
	if !versioned {
		_ = m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, _ = writeS3Error(m.conn, http.StatusBadRequest, "InvalidRequest", "Object Lock headers require a versioned bucket.", key)
		return lockState, true, fmt.Errorf("object lock headers on unversioned multipart upload: %w", syscall.EINVAL)
	}
	return lockState, true, nil
}

// ParseS3LockHeaders extracts the S3 object-lock header set from a request.
// Retention date must parse as the S3 timestamp shape (RFC 3339).
func ParseS3LockHeaders(req *http.Request) (storage.ObjectLockState, error) {
	var st storage.ObjectLockState
	if v := req.Header.Get("X-Amz-Object-Lock-Mode"); v != "" {
		switch strings.ToUpper(v) {
		case storage.LockModeCompliance, storage.LockModeGovernance:
			st.Mode = strings.ToUpper(v)
		default:
			return st, syscall.EINVAL
		}
	}
	if v := req.Header.Get("X-Amz-Object-Lock-Retain-Until-Date"); v != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(v))
		if err != nil {
			return st, syscall.EINVAL
		}
		st.RetainUntil = t.Unix()
	}
	if v := req.Header.Get("X-Amz-Object-Lock-Legal-Hold"); v != "" {
		switch strings.ToUpper(v) {
		case "ON":
			st.LegalHoldOn = true
		case "OFF":
		default:
			return st, syscall.EINVAL
		}
	}
	return st, nil
}

// ─── ?retention / ?legal-hold subresource APIs (R10 Phase 5, #938) ─────────
// Object-lock state is stored per version. These handlers mutate one facet at
// a time; the storage layer merges the other facet inside a single transaction.

const (
	msgLockReadBody         = "Failed to read request body."
	msgLockMalformedXML     = "The XML you provided was not well-formed or did not validate against our published schema."
	msgLockBucketNotEnabled = "Bucket is missing Object Lock Configuration."
)

// retentionXML is the S3 PUT ?retention body.
type retentionXML struct {
	XMLName         xml.Name `xml:"Retention"`
	XMLNS           string   `xml:"xmlns,attr"`
	Mode            string   `xml:"Mode"`
	RetainUntilDate string   `xml:"RetainUntilDate"`
}

// legalHoldXML is the S3 PUT/GET ?legal-hold document.
type legalHoldXML struct {
	XMLName xml.Name `xml:"LegalHold"`
	XMLNS   string   `xml:"xmlns,attr"`
	Status  string   `xml:"Status"`
}

// ParseRetentionXML validates a PUT ?retention body. Mode and RetainUntilDate
// are both required, matching the S3 schema.
func ParseRetentionXML(body []byte) (st storage.ObjectLockState, err error) {
	defer common.RecoverErr("ParseRetentionXML", &err)
	var doc retentionXML
	if err := xml.Unmarshal(body, &doc); err != nil {
		return st, fmt.Errorf("malformed retention document: %v: %w", err, syscall.EINVAL)
	}
	mode := strings.ToUpper(strings.TrimSpace(doc.Mode))
	if mode != storage.LockModeCompliance && mode != storage.LockModeGovernance {
		return st, syscall.EINVAL
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(doc.RetainUntilDate))
	if err != nil {
		return st, syscall.EINVAL
	}
	return storage.ObjectLockState{Mode: mode, RetainUntil: t.Unix()}, nil
}

// FormatRetentionXML renders GET ?retention. A version without retention
// yields the empty document rather than an error. Mode/date constants are
// schema values, so no escaping is needed.
func FormatRetentionXML(st storage.ObjectLockState) []byte {
	if st.Mode == "" || st.RetainUntil <= 0 {
		return []byte(`<Retention xmlns="` + s3XMLNS + `"></Retention>`)
	}
	return []byte(`<Retention xmlns="` + s3XMLNS + `"><Mode>` + st.Mode +
		`</Mode><RetainUntilDate>` + xmlIso8601(time.Unix(st.RetainUntil, 0)) +
		`</RetainUntilDate></Retention>`)
}

// ParseLegalHoldXML validates a PUT ?legal-hold body (ON or OFF).
func ParseLegalHoldXML(body []byte) (on bool, err error) {
	defer common.RecoverErr("ParseLegalHoldXML", &err)
	var doc legalHoldXML
	if err := xml.Unmarshal(body, &doc); err != nil {
		return false, fmt.Errorf("malformed legal-hold document: %v: %w", err, syscall.EINVAL)
	}
	switch strings.ToUpper(strings.TrimSpace(doc.Status)) {
	case "ON":
		return true, nil
	case "OFF":
		return false, nil
	}
	return false, syscall.EINVAL
}

// FormatLegalHoldXML renders GET ?legal-hold.
func FormatLegalHoldXML(on bool) []byte {
	status := "OFF"
	if on {
		status = "ON"
	}
	return []byte(`<LegalHold xmlns="` + s3XMLNS + `"><Status>` + status + `</Status></LegalHold>`)
}

// lockEnabledOrError verifies the bucket has object lock enabled, writing the
// S3 error when it does not.
func (m *S3Communicator) lockEnabledOrError(bucket, key string) error {
	enabled, err := m.lock.BucketObjectLockEnabled(bucket)
	if err != nil {
		_, _ = writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to read object lock configuration.", key)
		return fmt.Errorf("read bucket object lock status: %w", err)
	}
	if !enabled {
		_, _ = writeS3Error(m.conn, http.StatusBadRequest, "InvalidRequest", msgLockBucketNotEnabled, key)
		return ErrRequestHandled
	}
	return nil
}

// lockTargetVersion resolves the version a retention/legal-hold request
// targets: the explicit ?versionId, else the object's current version.
func (m *S3Communicator) lockTargetVersion(bucket, key, versionID string) (string, error) {
	if versionID != "" {
		return versionID, nil
	}
	if m.versioning == nil {
		_, _ = writeS3Error(m.conn, http.StatusNotImplemented, "NotImplemented", msgVersioningUnsupported, key)
		return "", ErrRequestHandled
	}
	rec, ok, err := m.versioning.LatestVersion(bucket, key)
	if err != nil {
		_, _ = writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to resolve the current version.", key)
		return "", fmt.Errorf("resolve latest version: %w", err)
	}
	if !ok || rec.IsDeleteMarker {
		_, _ = writeS3Error(m.conn, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.", key)
		return "", ErrRequestHandled
	}
	return rec.VersionID, nil
}

// lockTarget performs the shared precondition + target resolution for the
// retention/legal-hold subresource APIs.
func (m *S3Communicator) lockTarget(req *http.Request, bucket, key string) (string, error) {
	if err := m.lockEnabledOrError(bucket, key); err != nil {
		return "", err
	}
	return m.lockTargetVersion(bucket, key, req.URL.Query().Get("versionId"))
}

// readLockBody reads a bounded retention/legal-hold document.
func (m *S3Communicator) readLockBody(req *http.Request, key string) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(req.Body, 64*1024))
	if err != nil {
		_, _ = writeS3Error(m.conn, http.StatusBadRequest, "MalformedXML", msgLockReadBody, key)
		return nil, fmt.Errorf("read object lock body: %w", err)
	}
	return body, nil
}

// writeLockMutationError maps storage errors from a lock write to S3 responses.
func (m *S3Communicator) writeLockMutationError(err error, key string) error {
	switch err {
	case syscall.ENOENT:
		_, _ = writeS3Error(m.conn, http.StatusNotFound, "NoSuchVersion", "The specified version does not exist.", key)
		return ErrRequestHandled
	case syscall.EPERM:
		_, _ = writeS3Error(m.conn, http.StatusForbidden, "AccessDenied", "The requested object lock state change is not permitted.", key)
		return ErrRequestHandled
	case syscall.EINVAL:
		_, _ = writeS3Error(m.conn, http.StatusBadRequest, "InvalidArgument", "Invalid object lock values.", key)
		return ErrRequestHandled
	}
	_, _ = writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to store object lock state.", key)
	return fmt.Errorf("store object lock state: %w", err)
}

// writeLockMalformedXML answers 400 for an unparseable lock document.
func (m *S3Communicator) writeLockMalformedXML(key string) error {
	_, _ = writeS3Error(m.conn, http.StatusBadRequest, "MalformedXML", msgLockMalformedXML, key)
	return ErrRequestHandled
}

// writeLockMutationOK audits and acknowledges a lock mutation.
func (m *S3Communicator) writeLockMutationOK(op, key string) error {
	m.writeAuditLog(op, key, "success")
	return m.writeRawResponse("HTTP/1.1 200 OK\r\nContent-Length: 0"+s3ConnClose, nil, nil)
}

// handleGetObjectLockDoc reads the target version's lock record and writes the
// document produced by format.
func (m *S3Communicator) handleGetObjectLockDoc(req *http.Request, bucket, key string, format func(storage.ObjectLockState) []byte, describe string) (int, int64, error) {
	versionID, err := m.lockTarget(req, bucket, key)
	if err != nil {
		return 0, 0, err
	}
	st, _, err := m.lock.VersionLock(bucket, key, versionID)
	if err != nil {
		_, _ = writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to read object "+describe+".", key)
		return 0, 0, fmt.Errorf("read object %s: %w", describe, err)
	}
	body := format(st)
	return 0, 0, m.writeRawResponse(xmlResponseHeaders(len(body)), body, nil)
}

// handlePutObjectLockDoc runs the shared mutation flow: resolve target, read
// and parse the document, apply it to that version, acknowledge.
func (m *S3Communicator) handlePutObjectLockDoc(req *http.Request, bucket, key, op string, parse func([]byte) (func(versionID string) error, error)) (int, int64, error) {
	versionID, err := m.lockTarget(req, bucket, key)
	if err != nil {
		return 0, 0, err
	}
	body, rerr := m.readLockBody(req, key)
	if rerr != nil {
		return 0, 0, rerr
	}
	apply, perr := parse(body)
	if perr != nil {
		return 0, 0, m.writeLockMalformedXML(key)
	}
	if aerr := apply(versionID); aerr != nil {
		return 0, 0, m.writeLockMutationError(aerr, key)
	}
	return 0, 0, m.writeLockMutationOK(op, key)
}

// handleGetObjectRetention answers GET /{bucket}/{key}?retention.
func (m *S3Communicator) handleGetObjectRetention(req *http.Request, bucket, key string) (int, int64, error) {
	return m.handleGetObjectLockDoc(req, bucket, key, FormatRetentionXML, "retention")
}

// handleGetObjectLegalHold answers GET /{bucket}/{key}?legal-hold.
func (m *S3Communicator) handleGetObjectLegalHold(req *http.Request, bucket, key string) (int, int64, error) {
	return m.handleGetObjectLockDoc(req, bucket, key, func(st storage.ObjectLockState) []byte {
		return FormatLegalHoldXML(st.LegalHoldOn)
	}, "legal hold")
}

// handlePutObjectRetention answers PUT /{bucket}/{key}?retention.
func (m *S3Communicator) handlePutObjectRetention(req *http.Request, bucket, key string) (int, int64, error) {
	return m.handlePutObjectLockDoc(req, bucket, key, "PutObjectRetention", func(body []byte) (func(string) error, error) {
		next, perr := ParseRetentionXML(body)
		if perr != nil {
			return nil, perr
		}
		bypass := isGovernanceBypass(req)
		return func(versionID string) error {
			return m.lock.PutVersionRetention(bucket, key, versionID, next.Mode, next.RetainUntil, bypass)
		}, nil
	})
}

// handlePutObjectLegalHold answers PUT /{bucket}/{key}?legal-hold.
func (m *S3Communicator) handlePutObjectLegalHold(req *http.Request, bucket, key string) (int, int64, error) {
	return m.handlePutObjectLockDoc(req, bucket, key, "PutObjectLegalHold", func(body []byte) (func(string) error, error) {
		on, perr := ParseLegalHoldXML(body)
		if perr != nil {
			return nil, perr
		}
		return func(versionID string) error {
			return m.lock.PutVersionLegalHold(bucket, key, versionID, on)
		}, nil
	})
}

// objectLockSubresourceRoute dispatches ?retention / ?legal-hold for GET and
// PUT on an object key. handled=false means neither subresource is present.
func (m *S3Communicator) objectLockSubresourceRoute(req *http.Request, q url.Values, bucket, key string) (handled bool, code int, timestamp int64, err error) {
	retention := q.Has("retention")
	if !retention && !q.Has("legal-hold") {
		return false, 0, 0, nil
	}
	if m.lock == nil {
		return true, 0, 0, m.rejectObjectLockUnsupported(key)
	}
	if req.Method != "PUT" {
		if retention {
			code, timestamp, err = m.handleGetObjectRetention(req, bucket, key)
		} else {
			code, timestamp, err = m.handleGetObjectLegalHold(req, bucket, key)
		}
		return true, code, timestamp, err
	}
	if retention {
		code, timestamp, err = m.handlePutObjectRetention(req, bucket, key)
	} else {
		code, timestamp, err = m.handlePutObjectLegalHold(req, bucket, key)
	}
	return true, code, timestamp, err
}
