package transport

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

// S3 versioning surface (R10 Phase 1, #938): PUT/GET ?versioning, GET ?versions,
// GET/HEAD ?versionId, and versioned DELETE marker/permanent semantics.

// s3XMLNS is the S3 XML namespace identifier — an XML schema marker, never a
// fetched URL; plain HTTP is the canonical S3 namespace spelling.
const s3XMLNS = "http://s3.amazonaws.com/doc/2006-03-01/" // NOSONAR

// s3ConnClose is the standard S3 response connection terminator.
const s3ConnClose = "\r\nConnection: close\r\n\r\n"

// VersioningConfiguration is the S3 PUT ?versioning body.
type VersioningConfiguration struct {
	XMLName xml.Name `xml:"VersioningConfiguration"`
	XMLNS   string   `xml:"xmlns,attr"`
	Status  string   `xml:"Status"`
}

// ParseVersioningConfiguration extracts the versioning status from a
// PUT ?versioning body. A missing Status yields an empty string (the caller
// answers 400 MalformedXML).
func ParseVersioningConfiguration(body []byte) (status string, err error) {
	defer common.RecoverErr("ParseVersioningConfiguration", &err)
	var cfg VersioningConfiguration
	if err := xml.Unmarshal(body, &cfg); err != nil {
		return "", fmt.Errorf("malformed versioning configuration: %w: %v", syscall.EBADMSG, err)
	}
	return strings.TrimSpace(cfg.Status), nil
}

// FormatVersioningConfigurationXML renders the GET ?versioning response.
func FormatVersioningConfigurationXML(status string) []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<VersioningConfiguration xmlns="` + s3XMLNS + `">`)
	if status != "" {
		b.WriteString("<Status>" + status + "</Status>")
	}
	b.WriteString("</VersioningConfiguration>")
	return []byte(b.String())
}

// s3Version is one <Version> or <DeleteMarker> entry in ListVersionsResult.
type s3Version struct {
	XMLName      xml.Name
	Key          string `xml:"Key"`
	VersionID    string `xml:"VersionId"`
	IsLatest     bool   `xml:"IsLatest"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag,omitempty"`
	Size         uint64 `xml:"Size"`
	StorageClass string `xml:"StorageClass,omitempty"`
}

// ListVersionsResult is the GET ?versions response document.
type ListVersionsResult struct {
	XMLName         xml.Name    `xml:"ListVersionsResult"`
	XMLNS           string      `xml:"xmlns,attr"`
	Name            string      `xml:"Name"`
	Prefix          string      `xml:"Prefix"`
	KeyMarker       string      `xml:"KeyMarker"`
	VersionIDMarker string      `xml:"VersionIdMarker"`
	NextKeyMarker   string      `xml:"NextKeyMarker,omitempty"`
	NextVersionID   string      `xml:"NextVersionIdMarker,omitempty"`
	MaxKeys         int         `xml:"MaxKeys"`
	IsTruncated     bool        `xml:"IsTruncated"`
	Entries         []s3Version `xml:",any"`
}

// FormatListVersionsXML renders the GET ?versions response page.
func FormatListVersionsXML(bucket, prefix, keyMarker, versionIDMarker string, maxKeys int, page storage.VersionPage) (xmlBytes []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in FormatListVersionsXML: %v", r)
			err = syscall.EIO
		}
	}()

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString("<ListVersionsResult xmlns=\"")
	buf.WriteString(s3XMLNS)
	buf.WriteString("\">")

	buf.WriteString("<Name>")
	xmlEscape(&buf, bucket)
	buf.WriteString("</Name>")

	buf.WriteString("<Prefix>")
	xmlEscape(&buf, prefix)
	buf.WriteString("</Prefix>")

	buf.WriteString("<KeyMarker>")
	xmlEscape(&buf, keyMarker)
	buf.WriteString("</KeyMarker>")

	buf.WriteString("<VersionIdMarker>")
	xmlEscape(&buf, versionIDMarker)
	buf.WriteString("</VersionIdMarker>")

	if page.NextKeyMarker != "" {
		buf.WriteString("<NextKeyMarker>")
		xmlEscape(&buf, page.NextKeyMarker)
		buf.WriteString("</NextKeyMarker>")
	}

	if page.NextVersionID != "" {
		buf.WriteString("<NextVersionIdMarker>")
		xmlEscape(&buf, page.NextVersionID)
		buf.WriteString("</NextVersionIdMarker>")
	}

	buf.WriteString("<MaxKeys>")
	var intBuf [32]byte
	buf.Write(strconv.AppendInt(intBuf[:0], int64(maxKeys), 10))
	buf.WriteString("</MaxKeys>")

	buf.WriteString("<IsTruncated>")
	if page.IsTruncated {
		buf.WriteString("true")
	} else {
		buf.WriteString("false")
	}
	buf.WriteString("</IsTruncated>")

	// ⚡ Bolt: Eliminate dynamic string allocations and repeated formatting overhead
	// by replacing xml.MarshalIndent with a bytes.Buffer and using AppendFormat.
	var timeBuf [32]byte
	for _, rec := range page.Records {
		formatVersionRecord(&buf, rec, &timeBuf, &intBuf)
	}
	buf.WriteString("</ListVersionsResult>\n")
	return buf.Bytes(), nil
}

// formatVersionRecord writes a single version or delete marker entry directly to the buffer.
// Extracted to lower cognitive complexity.
func formatVersionRecord(buf *bytes.Buffer, rec storage.VersionRecord, timeBuf *[32]byte, intBuf *[32]byte) {
	if rec.IsDeleteMarker {
		buf.WriteString("<DeleteMarker>")
	} else {
		buf.WriteString("<Version>")
	}

	buf.WriteString("<Key>")
	xmlEscape(buf, rec.Key)
	buf.WriteString("</Key>")

	buf.WriteString("<VersionId>")
	xmlEscape(buf, rec.VersionID)
	buf.WriteString("</VersionId>")

	buf.WriteString("<IsLatest>")
	if rec.IsLatest {
		buf.WriteString("true")
	} else {
		buf.WriteString("false")
	}
	buf.WriteString("</IsLatest>")

	buf.WriteString("<LastModified>")
	t := time.Unix(0, rec.Timestamp).UTC()
	buf.Write(t.AppendFormat(timeBuf[:0], time.RFC3339Nano))
	buf.WriteString("</LastModified>")

	if !rec.IsDeleteMarker {
		buf.WriteString("<ETag>\"")
		xmlEscape(buf, rec.Hash)
		buf.WriteString("\"</ETag>")

		buf.WriteString("<Size>")
		buf.Write(strconv.AppendInt(intBuf[:0], int64(rec.Size), 10))
		buf.WriteString("</Size>")

		buf.WriteString("<StorageClass>STANDARD</StorageClass>")
	}

	if rec.IsDeleteMarker {
		buf.WriteString("</DeleteMarker>")
	} else {
		buf.WriteString("</Version>")
	}
}

// writeRawResponse writes a complete HTTP response with headers + body.
func (m *S3Communicator) writeRawResponse(headers string, body []byte, hdrErr error) error {
	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := m.conn.Write([]byte(headers)); err != nil {
		return fmt.Errorf("failed to write response headers: %v: %w", err, hdrErr)
	}
	if len(body) > 0 {
		if _, err := m.conn.Write(body); err != nil {
			return fmt.Errorf("failed to write response body: %v: %w", err, hdrErr)
		}
	}
	return nil
}

// xmlResponseHeaders builds the standard 200 XML response header block.
func xmlResponseHeaders(contentLength int) string {
	var b strings.Builder
	b.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/xml\r\nContent-Length: ")
	b.WriteString(strconv.Itoa(contentLength))
	b.WriteString(s3ConnClose)
	return b.String()
}

// handleGetVersioning answers GET /{bucket}?versioning.
func (m *S3Communicator) handleGetVersioning(bucket string) (int, int64, error) {
	status, configured, err := m.versioning.BucketVersioning(bucket)
	if err != nil {
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to read versioning configuration.", bucket)
		return 0, 0, fmt.Errorf("failed to read versioning status: %w", err)
	}
	body := FormatVersioningConfigurationXML(status)
	_ = configured
	return 0, 0, m.writeRawResponse(xmlResponseHeaders(len(body)), body, nil)
}

// handlePutVersioning answers PUT /{bucket}?versioning with the body already
// spooled by the gateway reader.
func (m *S3Communicator) handlePutVersioning(req *http.Request, bucket string) (int, int64, error) {
	body, err := io.ReadAll(io.LimitReader(req.Body, 64*1024))
	if err != nil {
		writeS3Error(m.conn, http.StatusBadRequest, "MalformedXML", "Failed to read request body.", bucket)
		return 0, 0, fmt.Errorf("failed to read versioning body: %w", err)
	}
	status, err := ParseVersioningConfiguration(body)
	if err != nil || (status != storage.VersioningEnabled && status != storage.VersioningSuspended) {
		writeS3Error(m.conn, http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed or did not validate against our published schema.", bucket)
		return 0, 0, fmt.Errorf("invalid versioning configuration: %w", syscall.EINVAL)
	}
	if err := m.versioning.SetBucketVersioning(bucket, status); err != nil {
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to store versioning configuration.", bucket)
		return 0, 0, fmt.Errorf("failed to set versioning status: %w", err)
	}
	m.writeAuditLog("PutVersioning", bucket, "success")
	return 0, 0, m.writeRawResponse("HTTP/1.1 200 OK\r\nContent-Length: 0"+s3ConnClose, nil, nil)
}

// handleGetVersions answers GET /{bucket}?versions with S3 pagination.
func (m *S3Communicator) handleGetVersions(bucket string, q url.Values) (int, int64, error) {
	prefix := q.Get("prefix")
	keyMarker := q.Get("key-marker")
	versionIDMarker := q.Get("version-id-marker")
	maxKeys := 1000
	if mk := q.Get("max-keys"); mk != "" {
		if n, err := strconv.Atoi(mk); err == nil && n > 0 {
			maxKeys = min(n, 1000)
		}
	}
	page, err := m.versioning.ListVersions(bucket, prefix, keyMarker, versionIDMarker, maxKeys)
	if err != nil {
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to list object versions.", bucket)
		return 0, 0, fmt.Errorf("failed to list versions: %w", err)
	}
	body, formatErr := FormatListVersionsXML(bucket, prefix, keyMarker, versionIDMarker, maxKeys, page)
	if formatErr != nil {
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to format versions response.", bucket)
		return 0, 0, fmt.Errorf("FormatListVersionsXML failed: %w", formatErr)
	}
	return 0, 0, m.writeRawResponse(xmlResponseHeaders(len(body)), body, nil)
}

// handleGetObjectVersion answers GET/HEAD /{bucket}/{key}?versionId=X by
// opening the exact version's content stream and echoing its metadata.
func (m *S3Communicator) handleGetObjectVersion(bucket, key, versionID string, isHead bool) (int, int64, error) {
	rc, rec, err := m.versioning.GetVersionContent(bucket, key, versionID)
	if err != nil {
		if err == syscall.ENOENT {
			writeS3Error(m.conn, http.StatusNotFound, "NoSuchVersion", "The specified version does not exist.", key)
			return 0, 0, ErrRequestHandled
		}
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to read the specified version.", key)
		return 0, 0, fmt.Errorf("failed to open version %q: %w", versionID, err)
	}
	defer rc.Close()

	var b strings.Builder
	b.WriteString("HTTP/1.1 200 OK\r\n")
	b.WriteString("ETag: \"" + rec.Hash + "\"\r\n")
	b.WriteString("x-amz-version-id: " + rec.VersionID + "\r\n")
	// R10 Phase 4 (#938): lock headers are advisory echo; a lookup failure
	// must not break the content path.
	if m.lock != nil {
		if st, ok, lerr := m.lock.VersionLock(bucket, key, versionID); lerr == nil && ok {
			injectObjectLockHeaders(&b, st)
		}
	}
	b.WriteString("Content-Length: " + strconv.FormatInt(rec.Size, 10) + "\r\n")
	b.WriteString("Connection: close\r\n")
	if isHead {
		b.WriteString("\r\n")
		if err := m.writeRawResponse(b.String(), nil, nil); err != nil {
			return 0, 0, err
		}
		return 0, 0, ErrRequestHandled
	}
	b.WriteString("Content-Type: application/octet-stream\r\n\r\n")
	if err := m.writeRawResponse(b.String(), nil, nil); err != nil {
		return 0, 0, err
	}
	if _, err := io.Copy(m.conn, rc); err != nil {
		return 0, 0, fmt.Errorf("failed to stream version content: %v: %w", err, syscall.EPIPE)
	}
	return 0, 0, ErrRequestHandled
}

// handleVersionedDelete implements DELETE on a versioned bucket: without
// versionId a delete marker is created (the object disappears for plain
// GETs); with versionId that version is permanently removed.
// isGovernanceBypass reports the S3 governance-retention bypass flag
// (x-amz-bypass-governance-retention, case-insensitive per S3 header rules).
// GOVERNANCE locks yield to it; COMPLIANCE retention and legal holds never do.
func isGovernanceBypass(req *http.Request) bool {
	return req.Header.Get("X-Amz-Bypass-Governance-Retention") == "true"
}

func (m *S3Communicator) handleVersionedDelete(bucket, key, versionID string, now int64, bypass bool) (int, int64, error) {
	if versionID == "" {
		return m.handleDeleteMarkerCreation(bucket, key)
	}
	return m.handlePermanentVersionDelete(bucket, key, versionID, now, bypass)
}

func (m *S3Communicator) handleDeleteMarkerCreation(bucket, key string) (int, int64, error) {
	// Delete marker: shadow the object by removing the flat mapping so
	// plain GETs 404, then record the marker version.
	if err := m.versioning.RepointNamespace(key, ""); err != nil {
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to delete object.", key)
		return 0, 0, fmt.Errorf("failed to remove mapping for delete marker: %w", err)
	}
	marker, err := m.versioning.RecordDeleteMarker(bucket, key)
	if err != nil {
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to create delete marker.", key)
		return 0, 0, fmt.Errorf("failed to record delete marker: %w", err)
	}
	hdr := "HTTP/1.1 204 No Content\r\nx-amz-delete-marker: true\r\nx-amz-version-id: " + marker + s3ConnClose
	m.writeAuditLog("DeleteObject", bucket+"/"+key, "success")
	return 0, 0, m.writeRawResponse(hdr, nil, nil)
}

func (m *S3Communicator) handlePermanentVersionDelete(bucket, key, versionID string, now int64, bypass bool) (int, int64, error) {
	// Permanent delete of one specific version.
	deleted, err := m.versioning.DeleteVersion(bucket, key, versionID, now, bypass)
	if err != nil {
		if err == syscall.ENOENT {
			writeS3Error(m.conn, http.StatusNotFound, "NoSuchVersion", "The specified version does not exist.", key)
			return 0, 0, ErrRequestHandled
		}
		if err == syscall.EACCES {
			// R10 Phase 4 (#938): the version is protected by retention or a legal hold.
			_, _ = writeS3Error(m.conn, http.StatusForbidden, "AccessDenied", "The specified version is protected by an object lock.", key)
			return 0, 0, ErrRequestHandled
		}
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to delete the specified version.", key)
		return 0, 0, fmt.Errorf("failed to delete version: %w", err)
	}

	// Keep the flat namespace aligned with the surviving latest record.
	if latest, ok, lerr := m.versioning.LatestVersion(bucket, key); lerr == nil && ok {
		if latest.IsDeleteMarker {
			_ = m.versioning.RepointNamespace(key, "")
		} else {
			_ = m.versioning.RepointNamespace(key, latest.Hash)
		}
	}
	marker := "false"
	if deleted.IsDeleteMarker {
		marker = "true"
	}
	hdr := "HTTP/1.1 204 No Content\r\nx-amz-delete-marker: " + marker + "\r\nx-amz-version-id: " + deleted.VersionID + s3ConnClose
	m.writeAuditLog("DeleteObjectVersion", bucket+"/"+key, "success")
	return 0, 0, m.writeRawResponse(hdr, nil, nil)
}
