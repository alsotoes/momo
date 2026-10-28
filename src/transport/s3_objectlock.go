package transport

// S3 Object Lock surface (R10, issue #938). Scope agreed with alsotoes:
//   - Object lock is enabled only at bucket creation (CreateBucket +
//     x-amz-bucket-object-lock-enabled); disabling is refused (409).
//   - Plain-PUT lock capture is a documented ceiling: the gateway response
//     precedes body streaming, so x-amz-object-lock-* on single PUTs cannot
//     reach the version record this phase — lock via multipart uploads.
//   - ?retention / ?legal-hold subresource APIs stay honest-501 this phase.

import (
	"bytes"
	"fmt"
	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"io"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// ObjectLockStore is the capability seam for object-lock state.
type ObjectLockStore interface {
	SetBucketObjectLock(bucket string) error
	BucketObjectLockEnabled(bucket string) (bool, error)
	PutVersionLock(bucket, key, versionID string, state storage.ObjectLockState) error
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
	if err := m.lock.PutVersionLock(bucket, key, versionID, st); err != nil {
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
