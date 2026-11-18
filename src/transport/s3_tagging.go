package transport

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// S3 object tagging (issue #1375): a small tag set stored beside the object's
// metadata in the S3 sidecar, in the wire form S3 itself uses on the
// x-amz-tagging header (URL-encoded key=value pairs). Storing the wire form means
// PUT ?tagging and a plain PUT carrying x-amz-tagging converge on one
// representation, and replication carries tags with the rest of the metadata.

const (
	// s3TaggingHeader is the metadata key holding the encoded tag set.
	s3TaggingHeader = "x-amz-tagging"
	// s3MaxTags is S3's limit on tags per object.
	s3MaxTags = 10
	// s3MaxTagKeyLen and s3MaxTagValueLen are S3's length limits.
	s3MaxTagKeyLen   = 128
	s3MaxTagValueLen = 256
)

// errInvalidTag is the sentinel for a tag set that violates an S3 rule.
var errInvalidTag = errors.New("invalid tag")

// tagSet is the ordered set of tags on an object.
type tagSet []objectTag

// objectTag is one key/value pair.
type objectTag struct {
	Key   string
	Value string
}

// validate checks the S3 tag rules: at most ten tags, key 1..128 characters,
// value at most 256, no duplicate keys, and no control characters in either.
func (ts tagSet) validate() error {
	if len(ts) > s3MaxTags {
		return errors.Join(errInvalidTag, syscall.EINVAL)
	}
	seen := make(map[string]struct{}, len(ts))
	for _, t := range ts {
		if t.Key == "" || len(t.Key) > s3MaxTagKeyLen || len(t.Value) > s3MaxTagValueLen {
			return errors.Join(errInvalidTag, syscall.EINVAL)
		}
		if hasControlChars(t.Key) || hasControlChars(t.Value) {
			return errors.Join(errInvalidTag, syscall.EINVAL)
		}
		if _, dup := seen[t.Key]; dup {
			return errors.Join(errInvalidTag, syscall.EINVAL)
		}
		seen[t.Key] = struct{}{}
	}
	return nil
}

// hasControlChars reports whether s contains a byte that has no business in a tag.
func hasControlChars(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// encode renders the set in S3's x-amz-tagging form (URL-encoded, '&'-joined).
// Entries are sorted by key so the stored form is deterministic.
func (ts tagSet) encode() string {
	sorted := make(tagSet, len(ts))
	copy(sorted, ts)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })

	var b strings.Builder
	for i, t := range sorted {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(t.Key))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(t.Value))
	}
	return b.String()
}

// errBadTagEncoding reports an x-amz-tagging value that is not a tag list.
var errBadTagEncoding = errors.New("malformed tag encoding")

// decodeTags parses the stored x-amz-tagging form. An empty value is an empty set.
func decodeTags(encoded string) (tagSet, error) {
	if encoded == "" {
		return nil, nil
	}
	pairs := strings.Split(encoded, "&")
	tags := make(tagSet, 0, len(pairs))
	for _, pair := range pairs {
		if pair == "" {
			continue
		}
		rawKey, rawValue, found := strings.Cut(pair, "=")
		if !found {
			return nil, errBadTagEncoding
		}
		key, kerr := url.QueryUnescape(rawKey)
		if kerr != nil {
			return nil, errBadTagEncoding
		}
		value, verr := url.QueryUnescape(rawValue)
		if verr != nil {
			return nil, errBadTagEncoding
		}
		tags = append(tags, objectTag{Key: key, Value: value})
	}
	if err := tags.validate(); err != nil {
		return nil, err
	}
	return tags, nil
}

// objectTags returns the tags stored for key (empty when none).
func (m *S3Communicator) objectTags(key string) tagSet {
	headers := m.storedS3Meta(key)
	if headers == nil {
		return nil
	}
	tags, err := decodeTags(headers[s3TaggingHeader])
	if err != nil {
		// Unreadable stored value: report no tags rather than failing a read.
		return nil
	}
	return tags
}

// storeObjectTags merges the encoded tag set into the object's S3 metadata,
// preserving every other header (checksums, content type, replication markers).
func (m *S3Communicator) storeObjectTags(key, encoded string) error {
	ps, ok := m.store.(interface {
		PutS3Meta(string, map[string]string) error
	})
	if !ok {
		m.writeTaggingError(http.StatusNotImplemented, "NotImplemented",
			"Object tagging requires a storage backend that persists S3 metadata.", key)
		return errors.Join(errTaggingUnsupported, syscall.ENOTSUP)
	}
	headers := map[string]string{}
	for k, v := range m.storedS3Meta(key) {
		headers[k] = v
	}
	if encoded == "" {
		delete(headers, s3TaggingHeader)
	} else {
		headers[s3TaggingHeader] = encoded
	}
	if err := ps.PutS3Meta(key, headers); err != nil {
		m.writeTaggingError(http.StatusInternalServerError, "InternalError",
			"The request failed due to an internal error.", key)
		return errors.Join(errTaggingUnsupported, syscall.EIO)
	}
	return nil
}

// errTaggingUnsupported marks a backend that cannot persist tags.
var errTaggingUnsupported = errors.New("object tagging unsupported")

// writeTaggingError answers an S3 error for a tagging route.
func (m *S3Communicator) writeTaggingError(status int, code, message, resource string) {
	_ = m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, _ = writeS3Error(m.conn, status, code, message, resource)
}

// objectExists reports whether key is present in the store.
//
// It deliberately uses Get rather than the optional Has: Has answers about a
// content *hash* in the objects bucket, not about an object name, and it is
// local-only — so it would report a healthy replicated object as missing.
func (m *S3Communicator) objectExists(key string) bool {
	if m.store == nil {
		return false
	}
	rc, _, err := m.store.Get(key)
	if err != nil {
		return false
	}
	_ = rc.Close()
	return true
}

// formatTaggingXML renders a tag set as the S3 Tagging document, escaping values.
func formatTaggingXML(tags tagSet) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<Tagging xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><TagSet>`)
	for _, t := range tags {
		b.WriteString("<Tag>")
		writeXMLString(&b, "Key", t.Key)
		writeXMLString(&b, "Value", t.Value)
		b.WriteString("</Tag>")
	}
	b.WriteString(`</TagSet></Tagging>`)
	return b.Bytes()
}

// handleGetObjectTagging answers GET /{bucket}/{key}?tagging.
func (m *S3Communicator) handleGetObjectTagging(bucket, key string) (requestedMode int, timestamp int64, err error) {
	if !m.validBucket(bucket) {
		m.writeTaggingError(http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
		return 0, 0, ErrRequestHandled
	}
	if !m.objectExists(key) {
		m.writeTaggingError(http.StatusNotFound, "NoSuchKey", "The specified key does not exist.", key)
		return 0, 0, ErrRequestHandled
	}
	body := formatTaggingXML(m.objectTags(key))
	_ = m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, _ = writeXMLResponse(m.conn, body)
	return 0, 0, ErrRequestHandled
}

// handlePutObjectTagging answers PUT /{bucket}/{key}?tagging.
func (m *S3Communicator) handlePutObjectTagging(req *http.Request, bucket, key string) (requestedMode int, timestamp int64, err error) {
	defer common.RecoverErr("S3 handlePutObjectTagging", &err)

	if !m.validBucket(bucket) {
		m.writeTaggingError(http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
		return 0, 0, ErrRequestHandled
	}
	if !m.objectExists(key) {
		m.writeTaggingError(http.StatusNotFound, "NoSuchKey", "The specified key does not exist.", key)
		return 0, 0, ErrRequestHandled
	}

	body, rerr := readBoundedBody(req, maxTaggingBodyBytes)
	if rerr != nil {
		m.writeTaggingError(http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed.", key)
		return 0, 0, rerr
	}
	tags, perr := parseTaggingXML(body)
	if perr != nil {
		m.writeTaggingError(http.StatusBadRequest, "InvalidTag", "The tag set is not valid.", key)
		return 0, 0, perr
	}
	if serr := m.storeObjectTags(key, tags.encode()); serr != nil {
		return 0, 0, serr
	}
	_ = m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, _ = m.conn.Write(emptyOKResponse(http.StatusOK))
	return 0, 0, ErrRequestHandled
}

// handleDeleteObjectTagging answers DELETE /{bucket}/{key}?tagging.
func (m *S3Communicator) handleDeleteObjectTagging(bucket, key string) (requestedMode int, timestamp int64, err error) {
	if !m.validBucket(bucket) {
		m.writeTaggingError(http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
		return 0, 0, ErrRequestHandled
	}
	if !m.objectExists(key) {
		m.writeTaggingError(http.StatusNotFound, "NoSuchKey", "The specified key does not exist.", key)
		return 0, 0, ErrRequestHandled
	}
	if serr := m.storeObjectTags(key, ""); serr != nil {
		return 0, 0, serr
	}
	_ = m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, _ = m.conn.Write(emptyOKResponse(http.StatusNoContent))
	return 0, 0, ErrRequestHandled
}

// emptyOKResponse renders a bodiless S3 success response.
func emptyOKResponse(status int) []byte {
	text := "OK"
	if status == http.StatusNoContent {
		text = "No Content"
	}
	return []byte("HTTP/1.1 " + strconv.Itoa(status) + " " + text +
		"\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
}

// validateTaggingHeader checks x-amz-tagging on an upload and rewrites the header
// into the canonical key-sorted form. A missing header is fine; an invalid one is
// refused before any payload is read, so nothing is stored on a bad request.
func (m *S3Communicator) validateTaggingHeader(req *http.Request) error {
	raw := req.Header.Get("X-Amz-Tagging")
	if raw == "" {
		return nil
	}
	tags, err := decodeTags(raw)
	if err != nil {
		m.writeTaggingError(http.StatusBadRequest, "InvalidTag", "The tag set is not valid.", "")
		return errors.Join(errInvalidTag, syscall.EBADMSG)
	}
	req.Header.Set("X-Amz-Tagging", tags.encode())
	return nil
}

// maxTaggingBodyBytes bounds a Tagging document: ten tags of 128+256 characters
// plus XML scaffolding fits comfortably, and a larger body is refused.
const maxTaggingBodyBytes = 16 * 1024

// parseTaggingXML parses and validates an S3 Tagging document.
func parseTaggingXML(body []byte) (tagSet, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var doc struct {
		TagSet struct {
			Tags []struct {
				Key   string `xml:"Key"`
				Value string `xml:"Value"`
			} `xml:"Tag"`
		} `xml:"TagSet"`
	}
	if err := dec.Decode(&doc); err != nil {
		return nil, errors.Join(errBadTagEncoding, syscall.EBADMSG)
	}
	tags := make(tagSet, 0, len(doc.TagSet.Tags))
	for _, t := range doc.TagSet.Tags {
		tags = append(tags, objectTag{Key: t.Key, Value: t.Value})
	}
	if err := tags.validate(); err != nil {
		return nil, err
	}
	return tags, nil
}

// readBoundedBody reads at most limit bytes of the request body.
func readBoundedBody(req *http.Request, limit int64) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	defer func() { _ = req.Body.Close() }()
	return io.ReadAll(io.LimitReader(req.Body, limit))
}
