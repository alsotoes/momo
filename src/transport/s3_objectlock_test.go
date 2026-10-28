package transport

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

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
	if err := ols.PutVersionLock("bucket", "key.txt", vid, st); err != nil {
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
		storage.ObjectLockState{LegalHoldOn: true}); err != nil {
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
