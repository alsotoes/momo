package transport

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/storage"
)

// s3ReqAuth is the shared gateway auth token literal used by the S3 tests. // notsecret
const s3ReqAuth = "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

// s3ReqWithHeaders renders a raw S3 request with extra headers.
func s3ReqWithHeaders(method, target, body string, hdrs map[string]string) string {
	b := method + " " + target + " HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + s3ReqAuth + "\r\n"
	for name, val := range hdrs {
		b += name + ": " + val + "\r\n"
	}
	if body != "" {
		b += fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	} else {
		b += "Content-Length: 0\r\n\r\n"
	}
	return b
}

type lockHeaderCase struct {
	name    string
	hdrs    map[string]string
	want    storage.ObjectLockState
	wantErr bool
}

func TestParseS3LockHeaders(t *testing.T) {
	cases := []lockHeaderCase{
		{name: "empty"},
		{
			name: "compliance_retention",
			hdrs: map[string]string{
				"X-Amz-Object-Lock-Mode":              "COMPLIANCE",
				"X-Amz-Object-Lock-Retain-Until-Date": "2030-01-02T03:04:05Z",
			},
			want: storage.ObjectLockState{Mode: storage.LockModeCompliance, RetainUntil: 1893553445},
		},
		{
			name: "governance_lowercase",
			hdrs: map[string]string{"X-Amz-Object-Lock-Mode": "governance"},
			want: storage.ObjectLockState{Mode: storage.LockModeGovernance},
		},
		{
			name: "legal_hold_on",
			hdrs: map[string]string{"X-Amz-Object-Lock-Legal-Hold": "on"},
			want: storage.ObjectLockState{LegalHoldOn: true},
		},
		{name: "legal_hold_off", hdrs: map[string]string{"X-Amz-Object-Lock-Legal-Hold": "OFF"}},
		{name: "bad_mode", hdrs: map[string]string{"X-Amz-Object-Lock-Mode": "BOGUS"}, wantErr: true},
		{name: "bad_date", hdrs: map[string]string{"X-Amz-Object-Lock-Retain-Until-Date": "next week"}, wantErr: true},
		{name: "bad_hold", hdrs: map[string]string{"X-Amz-Object-Lock-Legal-Hold": "MAYBE"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkLockHeaderCase(t, tc) })
	}
}

func checkLockHeaderCase(t *testing.T, tc lockHeaderCase) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1/bucket/key", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	for k, v := range tc.hdrs {
		req.Header.Set(k, v)
	}
	got, err := ParseS3LockHeaders(req)
	if tc.wantErr {
		if err == nil {
			t.Fatalf("expected error, got %+v", got)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != tc.want {
		t.Fatalf("got %+v want %+v", got, tc.want)
	}
}

func TestObjectLockConfigurationXML(t *testing.T) {
	raw := FormatObjectLockConfigurationXML()
	if err := ParseObjectLockConfiguration(raw); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	disabled := []byte(`<ObjectLockConfiguration><ObjectLockEnabled>Disabled</ObjectLockEnabled></ObjectLockConfiguration>`)
	if err := ParseObjectLockConfiguration(disabled); err == nil {
		t.Fatal("expected error for Disabled document")
	}
}

func TestS3ObjectLock_UnsupportedStore(t *testing.T) {
	for _, req := range []string{
		s3Req("GET", "/bucket?object-lock", ""),
		s3Req("PUT", "/bucket?object-lock", `<ObjectLockConfiguration><ObjectLockEnabled>Enabled</ObjectLockEnabled></ObjectLockConfiguration>`),
	} {
		resp, _ := runS3RequestCaptureBucket(t, req, &mockStore{}, "bucket")
		if !strings.Contains(resp, "501") || !strings.Contains(resp, "NotImplemented") {
			t.Fatalf("expected 501 NotImplemented:\n%s", resp)
		}
	}
}

func TestS3ObjectLock_GetMissingConfig(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	resp, _ := runS3RequestCaptureBucket(t, s3Req("GET", "/bucket?object-lock", ""), store, "bucket")
	if !strings.Contains(resp, "404") || !strings.Contains(resp, "NoSuchObjectLockConfiguration") {
		t.Fatalf("expected 404 NoSuchObjectLockConfiguration:\n%s", resp)
	}
}

func TestS3ObjectLock_PutRefused(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	body := `<ObjectLockConfiguration><ObjectLockEnabled>Enabled</ObjectLockEnabled></ObjectLockConfiguration>`
	resp, _ := runS3RequestCaptureBucket(t, s3Req("PUT", "/bucket?object-lock", body), store, "bucket")
	if !strings.Contains(resp, "409") || !strings.Contains(resp, "InvalidBucketState") {
		t.Fatalf("expected 409 InvalidBucketState:\n%s", resp)
	}
	enabled, err := store.(ObjectLockStore).BucketObjectLockEnabled("bucket")
	if err != nil || enabled {
		t.Fatalf("lock must not be enabled by a refused PUT: enabled=%v err=%v", enabled, err)
	}
}

func TestS3ObjectLock_PutMalformed(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	resp, _ := runS3RequestCaptureBucket(t, s3Req("PUT", "/bucket?object-lock", "<nope/>"), store, "bucket")
	if !strings.Contains(resp, "400") || !strings.Contains(resp, "MalformedXML") {
		t.Fatalf("expected 400 MalformedXML:\n%s", resp)
	}
}

func TestS3ObjectLock_CreateBucketEnables(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	req := s3ReqWithHeaders("PUT", "/bucket", "", map[string]string{
		"X-Amz-Bucket-Object-Lock-Enabled": "true",
	})
	resp, err := runS3RequestCaptureBucket(t, req, store, "bucket")
	if err != nil && err != ErrRequestHandled || !strings.Contains(resp, "HTTP/1.1 200 OK") {
		t.Fatalf("CreateBucket with lock failed: resp=%s err=%v", resp, err)
	}
	enabled, lerr := store.(ObjectLockStore).BucketObjectLockEnabled("bucket")
	if lerr != nil || !enabled {
		t.Fatalf("bucket lock not enabled: enabled=%v err=%v", enabled, lerr)
	}
	// Lock implies versioning-enabled (S3 semantics).
	versioned, _ := store.(VersioningStore).IsVersioned("bucket")
	if !versioned {
		t.Fatal("enabling object lock must imply versioning")
	}

	resp, _ = runS3RequestCaptureBucket(t, s3Req("GET", "/bucket?object-lock", ""), store, "bucket")
	if !strings.Contains(resp, "<ObjectLockEnabled>Enabled</ObjectLockEnabled>") {
		t.Fatalf("GET object-lock must report Enabled:\n%s", resp)
	}
}

func TestS3ObjectLock_GetVersionHeaders(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	vs := store.(VersioningStore)
	ols := store.(ObjectLockStore)

	if err := vs.SetBucketVersioning("bucket", storage.VersioningEnabled); err != nil {
		t.Fatalf("enable versioning: %v", err)
	}
	hash := strings.Repeat("a", 64)
	if err := store.Put("key.txt", hash, 3, "", strings.NewReader("abc")); err != nil {
		t.Fatalf("put: %v", err)
	}
	vid, err := vs.RecordVersion("bucket", "key.txt", hash, 3)
	if err != nil {
		t.Fatalf("record version: %v", err)
	}
	st := storage.ObjectLockState{Mode: storage.LockModeCompliance, RetainUntil: 1893553445, LegalHoldOn: true}
	if err := ols.PutVersionLock("bucket", "key.txt", vid, st, false); err != nil {
		t.Fatalf("put version lock: %v", err)
	}

	resp, _ := runS3RequestCaptureBucket(t, s3Req("HEAD", "/bucket/key.txt?versionId="+vid, ""), store, "bucket")
	for _, want := range []string{
		"x-amz-object-lock-mode: COMPLIANCE",
		"x-amz-object-lock-retain-until-date: 2030-01-02T03:04:05Z",
		"x-amz-object-lock-legal-hold-status: ON",
	} {
		if !strings.Contains(resp, want) {
			t.Fatalf("missing %q in response:\n%s", want, resp)
		}
	}
}

func TestS3ObjectLock_DeleteDenied(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	vs := store.(VersioningStore)
	ols := store.(ObjectLockStore)

	if err := vs.SetBucketVersioning("bucket", storage.VersioningEnabled); err != nil {
		t.Fatalf("enable versioning: %v", err)
	}
	hash := strings.Repeat("b", 64)
	if err := store.Put("key.txt", hash, 3, "", strings.NewReader("abc")); err != nil {
		t.Fatalf("put: %v", err)
	}
	vid, err := vs.RecordVersion("bucket", "key.txt", hash, 3)
	if err != nil {
		t.Fatalf("record version: %v", err)
	}
	if err := ols.PutVersionLock("bucket", "key.txt", vid,
		storage.ObjectLockState{LegalHoldOn: true}, false); err != nil {
		t.Fatalf("put version lock: %v", err)
	}

	resp, _ := runS3RequestCaptureBucket(t, s3Req("DELETE", "/bucket/key.txt?versionId="+vid, ""), store, "bucket")
	if !strings.Contains(resp, "403") || !strings.Contains(resp, "AccessDenied") {
		t.Fatalf("expected 403 AccessDenied for a held version:\n%s", resp)
	}
}

// ─── Multipart object-lock capture ─────────────────────────────────────────

func TestS3ObjectLock_MultipartCapture(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	vs := store.(VersioningStore)
	if err := vs.SetBucketVersioning("bucket", storage.VersioningEnabled); err != nil {
		t.Fatalf("enable versioning: %v", err)
	}

	uploadID := "uuid-lock-1"
	muUploads.Lock()
	uploads[uploadID] = &multipartUpload{
		bucket: "bucket",
		key:    "locked.txt",
		parts:  []multipartPart{{partNumber: 1, data: []byte("worm-bytes")}},
	}
	muUploads.Unlock()
	defer func() {
		muUploads.Lock()
		delete(uploads, uploadID)
		muUploads.Unlock()
	}()

	req := s3ReqWithHeaders("POST", "/bucket/locked.txt?uploadId="+uploadID, "", map[string]string{
		"X-Amz-Object-Lock-Mode":              "COMPLIANCE",
		"X-Amz-Object-Lock-Retain-Until-Date": "2030-01-02T03:04:05Z",
	})
	resp, err := runS3RequestCaptureBucket(t, req, store, "bucket")
	if err != nil && err != ErrRequestHandled {
		t.Fatalf("complete multipart: resp=%s err=%v", resp, err)
	}
	if !strings.Contains(resp, "HTTP/1.1 200 OK") || !strings.Contains(resp, "<VersionId>") {
		t.Fatalf("expected 200 with VersionId:\n%s", resp)
	}

	vid := extractXMLValue(resp, "VersionId")
	st, ok, lerr := store.(ObjectLockStore).VersionLock("bucket", "locked.txt", vid)
	if lerr != nil || !ok {
		t.Fatalf("lock record missing: ok=%v err=%v", ok, lerr)
	}
	if st.Mode != storage.LockModeCompliance || st.RetainUntil != 1893553445 {
		t.Fatalf("unexpected lock state: %+v", st)
	}
}

func TestS3ObjectLock_MultipartUnversionedRejected(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	uploadID := "uuid-lock-2"
	muUploads.Lock()
	uploads[uploadID] = &multipartUpload{
		bucket: "bucket",
		key:    "plain.txt",
		parts:  []multipartPart{{partNumber: 1, data: []byte("bytes")}},
	}
	muUploads.Unlock()
	defer func() {
		muUploads.Lock()
		delete(uploads, uploadID)
		muUploads.Unlock()
	}()

	req := s3ReqWithHeaders("POST", "/bucket/plain.txt?uploadId="+uploadID, "", map[string]string{
		"X-Amz-Object-Lock-Legal-Hold": "ON",
	})
	resp, _ := runS3RequestCaptureBucket(t, req, store, "bucket")
	if !strings.Contains(resp, "400") || !strings.Contains(resp, "InvalidRequest") {
		t.Fatalf("expected 400 InvalidRequest on unversioned bucket:\n%s", resp)
	}
}

func extractXMLValue(body, tag string) string {
	open := "<" + tag + ">"
	start := strings.Index(body, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(body[start:], "</"+tag+">")
	if end < 0 {
		return ""
	}
	return body[start : start+end]
}

func TestRetentionXML(t *testing.T) {
	until := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	body := []byte(`<Retention><Mode>governance</Mode><RetainUntilDate>` + until.Format(time.RFC3339) + `</RetainUntilDate></Retention>`)
	st, err := ParseRetentionXML(body)
	if err != nil {
		t.Fatalf("ParseRetentionXML: %v", err)
	}
	if st.Mode != storage.LockModeGovernance || st.RetainUntil != until.Unix() {
		t.Fatalf("unexpected retention state: %+v", st)
	}
	formatted := string(FormatRetentionXML(st))
	if !strings.Contains(formatted, "<Mode>GOVERNANCE</Mode>") || !strings.Contains(formatted, until.Format(time.RFC3339)) {
		t.Fatalf("unexpected retention XML: %s", formatted)
	}
	if got := string(FormatRetentionXML(storage.ObjectLockState{})); !strings.Contains(got, "<Retention") || strings.Contains(got, "<Mode>") {
		t.Fatalf("empty retention must render an empty document: %s", got)
	}
}

func TestRetentionXML_Invalid(t *testing.T) {
	bad := [][]byte{
		[]byte(`<Retention></Retention>`),
		[]byte(`<Retention><Mode>PARANOID</Mode><RetainUntilDate>2030-01-02T03:04:05Z</RetainUntilDate></Retention>`),
		[]byte(`<Retention><Mode>GOVERNANCE</Mode><RetainUntilDate>next week</RetainUntilDate></Retention>`),
		[]byte(`<NotRetention/>`),
	}
	for _, b := range bad {
		if _, err := ParseRetentionXML(b); err == nil {
			t.Fatalf("expected error for %s", b)
		}
	}
}

func TestLegalHoldXML(t *testing.T) {
	if on, err := ParseLegalHoldXML([]byte(`<LegalHold><Status>on</Status></LegalHold>`)); err != nil || !on {
		t.Fatalf("legal hold ON: on=%v err=%v", on, err)
	}
	if on, err := ParseLegalHoldXML([]byte(`<LegalHold><Status>OFF</Status></LegalHold>`)); err != nil || on {
		t.Fatalf("legal hold OFF: on=%v err=%v", on, err)
	}
	if _, err := ParseLegalHoldXML([]byte(`<LegalHold><Status>MAYBE</Status></LegalHold>`)); err == nil {
		t.Fatal("expected error for invalid legal-hold status")
	}
	if got := string(FormatLegalHoldXML(true)); !strings.Contains(got, "<Status>ON</Status>") {
		t.Fatalf("unexpected legal-hold XML: %s", got)
	}
}

func s3LockReq(t *testing.T, store storage.Store, method, target, body string, hdrs map[string]string) string {
	t.Helper()
	req := s3Req(method, target, body)
	if hdrs != nil {
		req = s3ReqWithHeaders(method, target, body, hdrs)
	}
	resp, _ := runS3RequestCaptureBucket(t, req, store, "bucket")
	return resp
}

func retentionDoc(mode string, until time.Time) string {
	return `<Retention><Mode>` + mode + `</Mode><RetainUntilDate>` + until.Format(time.RFC3339) + `</RetainUntilDate></Retention>`
}

func newLockedVersionStack(t *testing.T, key, payload string) (storage.Store, string, int64) {
	t.Helper()
	store, _ := newVersionedS3Stack(t)
	ols := store.(ObjectLockStore)
	if err := ols.SetBucketObjectLock("bucket"); err != nil {
		t.Fatalf("enable lock: %v", err)
	}
	hash := strings.Repeat(payload, 64)
	if err := store.Put(key, hash, 3, "", strings.NewReader("abc")); err != nil {
		t.Fatalf("put: %v", err)
	}
	vid, err := store.(VersioningStore).RecordVersion("bucket", key, hash, 3)
	if err != nil {
		t.Fatalf("record version: %v", err)
	}
	return store, vid, time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second).Unix()
}

func TestS3ObjectLock_RetentionAPI(t *testing.T) {
	store, _, untilUnix := newLockedVersionStack(t, "key.txt", "b")
	until := time.Unix(untilUnix, 0).UTC()
	bypass := map[string]string{"X-Amz-Bypass-Governance-Retention": "true"}

	if resp := s3LockReq(t, store, "PUT", "/bucket/key.txt?retention", retentionDoc("GOVERNANCE", until), nil); !strings.Contains(resp, "200") {
		t.Fatalf("PUT retention failed:\n%s", resp)
	}
	resp := s3LockReq(t, store, "GET", "/bucket/key.txt?retention", "", nil)
	if !strings.Contains(resp, "GOVERNANCE") || !strings.Contains(resp, until.Format(time.RFC3339)) {
		t.Fatalf("GET retention mismatch:\n%s", resp)
	}

	// Shortening GOVERNANCE retention requires the bypass header.
	shorter := retentionDoc("GOVERNANCE", until.Add(-24*time.Hour))
	if resp := s3LockReq(t, store, "PUT", "/bucket/key.txt?retention", shorter, nil); !strings.Contains(resp, "403") || !strings.Contains(resp, "AccessDenied") {
		t.Fatalf("shorten without bypass must be denied:\n%s", resp)
	}
	if resp := s3LockReq(t, store, "PUT", "/bucket/key.txt?retention", shorter, bypass); !strings.Contains(resp, "200") {
		t.Fatalf("shorten with bypass must succeed:\n%s", resp)
	}
}

func TestS3ObjectLock_LegalHoldAPI(t *testing.T) {
	store, vid, untilUnix := newLockedVersionStack(t, "hold.txt", "d")
	until := time.Unix(untilUnix, 0).UTC()
	bypass := map[string]string{"X-Amz-Bypass-Governance-Retention": "true"}
	deleteVersion := "/bucket/hold.txt?versionId=" + vid

	if resp := s3LockReq(t, store, "PUT", "/bucket/hold.txt?retention", retentionDoc("GOVERNANCE", until), nil); !strings.Contains(resp, "200") {
		t.Fatalf("PUT retention failed:\n%s", resp)
	}
	if resp := s3LockReq(t, store, "PUT", "/bucket/hold.txt?legal-hold", `<LegalHold><Status>ON</Status></LegalHold>`, nil); !strings.Contains(resp, "200") {
		t.Fatalf("PUT legal-hold ON failed:\n%s", resp)
	}
	if resp := s3LockReq(t, store, "GET", "/bucket/hold.txt?legal-hold", "", nil); !strings.Contains(resp, "<Status>ON</Status>") {
		t.Fatalf("GET legal-hold should be ON:\n%s", resp)
	}
	if resp := s3LockReq(t, store, "DELETE", deleteVersion, "", bypass); !strings.Contains(resp, "403") {
		t.Fatalf("legal hold must block deletion even with governance bypass:\n%s", resp)
	}

	// Legal hold OFF keeps the GOVERNANCE retention intact.
	if resp := s3LockReq(t, store, "PUT", "/bucket/hold.txt?legal-hold", `<LegalHold><Status>OFF</Status></LegalHold>`, nil); !strings.Contains(resp, "200") {
		t.Fatalf("PUT legal-hold OFF failed:\n%s", resp)
	}
	if resp := s3LockReq(t, store, "GET", "/bucket/hold.txt?retention", "", nil); !strings.Contains(resp, "GOVERNANCE") {
		t.Fatalf("retention must survive legal-hold changes:\n%s", resp)
	}
	if resp := s3LockReq(t, store, "DELETE", deleteVersion, "", bypass); !strings.Contains(resp, "204") {
		t.Fatalf("delete with governance bypass after hold off must succeed:\n%s", resp)
	}
}

func TestS3ObjectLock_ComplianceCannotShorten(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	vs := store.(VersioningStore)
	ols := store.(ObjectLockStore)
	if err := ols.SetBucketObjectLock("bucket"); err != nil {
		t.Fatalf("enable lock: %v", err)
	}
	hash := strings.Repeat("c", 64)
	if err := store.Put("c.txt", hash, 3, "", strings.NewReader("abc")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := vs.RecordVersion("bucket", "c.txt", hash, 3); err != nil {
		t.Fatalf("record version: %v", err)
	}
	until := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second)
	body := `<Retention><Mode>COMPLIANCE</Mode><RetainUntilDate>` + until.Format(time.RFC3339) + `</RetainUntilDate></Retention>`
	resp, _ := runS3RequestCaptureBucket(t, s3Req("PUT", "/bucket/c.txt?retention", body), store, "bucket")
	if !strings.Contains(resp, "200") {
		t.Fatalf("PUT compliance retention failed:\n%s", resp)
	}
	shorter := `<Retention><Mode>COMPLIANCE</Mode><RetainUntilDate>` + until.Add(-time.Hour).Format(time.RFC3339) + `</RetainUntilDate></Retention>`
	bypassHdrs := map[string]string{"X-Amz-Bypass-Governance-Retention": "true"}
	resp, _ = runS3RequestCaptureBucket(t, s3ReqWithHeaders("PUT", "/bucket/c.txt?retention", shorter, bypassHdrs), store, "bucket")
	if !strings.Contains(resp, "403") {
		t.Fatalf("COMPLIANCE must ignore governance bypass when shortened:\n%s", resp)
	}
}

func TestS3ObjectLock_RetentionRequiresLockEnabled(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	if err := store.(VersioningStore).SetBucketVersioning("bucket", storage.VersioningEnabled); err != nil {
		t.Fatalf("enable versioning: %v", err)
	}
	body := `<Retention><Mode>GOVERNANCE</Mode><RetainUntilDate>2030-01-02T03:04:05Z</RetainUntilDate></Retention>`
	resp, _ := runS3RequestCaptureBucket(t, s3Req("PUT", "/bucket/key.txt?retention", body), store, "bucket")
	if !strings.Contains(resp, "400") || !strings.Contains(resp, "InvalidRequest") {
		t.Fatalf("retention on a non-lock bucket must be 400 InvalidRequest:\n%s", resp)
	}
}
