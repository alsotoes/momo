package transport

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/storage"
)

func TestParseVersioningConfiguration(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{
			name: "enabled",
			body: `<?xml version="1.0" encoding="UTF-8"?><VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`,
			want: "Enabled",
		},
		{
			name: "suspended",
			body: `<VersioningConfiguration><Status>Suspended</Status></VersioningConfiguration>`,
			want: "Suspended",
		},
		{
			name:    "missing status",
			body:    `<VersioningConfiguration></VersioningConfiguration>`,
			want:    "",
			wantErr: false, // empty status → caller answers MalformedXML
		},
		{
			name:    "malformed xml",
			body:    `<VersioningConfiguration><Status>Enabled</VersioningConfiguration>`,
			wantErr: true,
		},
		{
			name:    "wrong element",
			body:    `<SomethingElse><Status>Enabled</Status></SomethingElse>`,
			want:    "",
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseVersioningConfiguration([]byte(tc.body))
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got status %q", got)
			}
			if !tc.wantErr && got != tc.want {
				t.Fatalf("expected %q, got %q (err=%v)", tc.want, got, err)
			}
		})
	}
}

func TestFormatVersioningConfigurationXML(t *testing.T) {
	body := string(FormatVersioningConfigurationXML(storage.VersioningEnabled))
	if !strings.Contains(body, "<Status>Enabled</Status>") {
		t.Fatalf("missing status: %s", body)
	}
	if !strings.Contains(body, `xmlns="http://s3.amazonaws.com/doc/2006-03-01/"`) {
		t.Fatalf("missing namespace: %s", body)
	}

	// Unconfigured bucket: no Status element (S3 omits it).
	empty := string(FormatVersioningConfigurationXML(""))
	if strings.Contains(empty, "<Status>") {
		t.Fatalf("unconfigured bucket must omit Status: %s", empty)
	}
}

func TestFormatListVersionsXML(t *testing.T) {
	now := time.Now().UnixNano()
	page := storage.VersionPage{
		IsTruncated:   true,
		NextKeyMarker: "logs/app.log",
		NextVersionID: "abcd-1234",
		Records: []storage.VersionRecord{
			{Key: "logs/app.log", VersionID: "v-2", Hash: "hash-b", Size: 42, Timestamp: now, IsLatest: true},
			{Key: "logs/app.log", VersionID: "v-1", Hash: "hash-a", Size: 10, Timestamp: now - 100},
			{Key: "logs/app.log", VersionID: "v-m", Timestamp: now - 50, IsLatest: false, IsDeleteMarker: true},
		},
	}
	raw := FormatListVersionsXML("bucket", "logs/", "", "", 1000, page)
	var result ListVersionsResult
	if err := xml.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, raw)
	}
	if !result.IsTruncated || result.NextKeyMarker != "logs/app.log" || result.NextVersionID != "abcd-1234" {
		t.Fatalf("pagination fields wrong: %+v", result)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result.Entries))
	}

	// Entry 0: data version, latest, ETag present.
	v0 := result.Entries[0]
	if v0.XMLName.Local != "Version" || !v0.IsLatest || v0.ETag != `"hash-b"` || v0.Size != 42 {
		t.Fatalf("entry 0 wrong: %+v", v0)
	}
	// Entry 2: delete marker — different XML element, no ETag, zero size.
	v2 := result.Entries[2]
	if v2.XMLName.Local != "DeleteMarker" {
		t.Fatalf("entry 2 must be a DeleteMarker, got %s", v2.XMLName.Local)
	}
	if v2.ETag != "" || v2.Size != 0 {
		t.Fatalf("delete marker must carry no ETag/size: %+v", v2)
	}
	if _, err := xml.Marshal(result.Entries[0].LastModified); err != nil {
		t.Fatalf("last-modified not a string: %v", err)
	}
	if !strings.Contains(v0.LastModified, "T") {
		t.Fatalf("last-modified not RFC3339: %s", v0.LastModified)
	}
}

func TestFormatListVersionsXMLEmpty(t *testing.T) {
	raw := FormatListVersionsXML("bucket", "", "", "", 1000, storage.VersionPage{})
	if !bytes.Contains(raw, []byte("<IsTruncated>false</IsTruncated>")) {
		t.Fatalf("expected empty truncated=false page: %s", raw)
	}
	if bytes.Contains(raw, []byte("<Version>")) || bytes.Contains(raw, []byte("<DeleteMarker")) {
		t.Fatalf("empty page must have no entries: %s", raw)
	}
}

// ─── Full-stack versioned-flow tests (real CASStore through the gateway) ───

func newVersionedS3Stack(t *testing.T) (storage.Store, func()) {
	t.Helper()
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store, func() {}
}

func s3Req(method, target, body string) string {
	b := method + " " + target + " HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6\r\n"
	if body != "" {
		b += "Content-Length: " + itoa(len(body)) + "\r\n\r\n" + body
	} else {
		b += "\r\n"
	}
	return b
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestS3Versioning_FullStackFlow(t *testing.T) {
	store, _ := newVersionedS3Stack(t)

	// 1. Enable versioning on the bucket.
	resp, err := runS3RequestCaptureBucket(t, s3Req("PUT", "/bucket?versioning",
		`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`), store, "bucket")
	if err != nil || !strings.Contains(resp, "HTTP/1.1 200 OK") {
		t.Fatalf("PUT versioning failed: resp=%s err=%v", resp, err)
	}
	if enabled, verr := store.(interface {
		IsVersioned(string) (bool, error)
	}).IsVersioned("bucket"); verr != nil || !enabled {
		t.Fatalf("bucket should be versioned: enabled=%v err=%v", enabled, verr)
	}

	// 2. Record two versions directly (PUT-object version recording happens at
	// the server ingest layer; the gateway surface is exercised here).
	h1 := strings.Repeat("a", 64)
	h2 := strings.Repeat("b", 64)
	if err := store.Put("docs/file.txt", h1, 3, "", strings.NewReader("abc")); err != nil {
		t.Fatalf("Put v1: %v", err)
	}
	vs := store.(VersioningStore)
	vid1, err := vs.RecordVersion("bucket", "docs/file.txt", h1, 3)
	if err != nil {
		t.Fatalf("RecordVersion v1: %v", err)
	}
	if err := store.Put("docs/file.txt", h2, 5, "", strings.NewReader("abcde")); err != nil {
		t.Fatalf("Put v2: %v", err)
	}
	vid2, err := vs.RecordVersion("bucket", "docs/file.txt", h2, 5)
	if err != nil {
		t.Fatalf("RecordVersion v2: %v", err)
	}

	// 3. GET ?versions — both versions listed, newest first, IsLatest on v2.
	resp, err = runS3RequestCaptureBucket(t, s3Req("GET", "/bucket?versions", ""), store, "bucket")
	if err != nil {
		t.Fatalf("GET versions: %v", err)
	}
	if !strings.Contains(resp, "<VersionId>"+vid2+"</VersionId>") ||
		!strings.Contains(resp, "<VersionId>"+vid1+"</VersionId>") {
		t.Fatalf("versions response missing ids:\n%s", resp)
	}
	v2Idx, v1Idx := strings.Index(resp, vid2), strings.Index(resp, vid1)
	if v2Idx > v1Idx {
		t.Fatalf("newest version must come first:\n%s", resp)
	}
	if !strings.Contains(resp, "<IsLatest>true</IsLatest>") {
		t.Fatalf("missing IsLatest:\n%s", resp)
	}

	// 4. GET the specific old version — its ETag is the old hash.
	resp, err = runS3RequestCaptureBucket(t, s3Req("GET", "/bucket/docs/file.txt?versionId="+vid1, ""), store, "bucket")
	if err != nil && err != ErrRequestHandled {
		t.Fatalf("GET versionId: resp=%s err=%v", resp, err)
	}
	if !strings.Contains(resp, `ETag: "`) {
		t.Fatalf("missing ETag in:\n%s", resp)
	}
	if !strings.Contains(resp, h1[:16]) {
		t.Fatalf("expected old version ETag %s... in:\n%s", h1[:16], resp)
	}

	// 5. DELETE (no versionId) → delete marker + object disappears.
	resp, err = runS3RequestCaptureBucket(t, s3Req("DELETE", "/bucket/docs/file.txt", ""), store, "bucket")
	if err != nil || !strings.Contains(resp, "HTTP/1.1 204 No Content") ||
		!strings.Contains(resp, "x-amz-delete-marker: true") {
		t.Fatalf("versioned delete: resp=%s err=%v", resp, err)
	}
	if _, _, err := store.Get("docs/file.txt"); err == nil {
		t.Fatal("object must be invisible after delete marker")
	}
	rec, ok, lerr := vs.LatestVersion("bucket", "docs/file.txt")
	if lerr != nil || !ok || !rec.IsDeleteMarker {
		t.Fatalf("expected delete marker latest: %+v ok=%v err=%v", rec, ok, lerr)
	}

	// 6. DELETE with the marker's versionId → object revives with v2.
	resp, err = runS3RequestCaptureBucket(t, s3Req("DELETE", "/bucket/docs/file.txt?versionId="+rec.VersionID, ""), store, "bucket")
	if err != nil || !strings.Contains(resp, "HTTP/1.1 204 No Content") {
		t.Fatalf("marker delete: resp=%s err=%v", resp, err)
	}
	meta, gerr := store.GetMeta("docs/file.txt")
	if gerr != nil || meta.Hash != h2 {
		t.Fatalf("object must revive with v2 after marker delete: %+v err=%v", meta, gerr)
	}

	// 7. DELETE with v1's versionId → v1 gone permanently, v2 untouched.
	resp, err = runS3RequestCaptureBucket(t, s3Req("DELETE", "/bucket/docs/file.txt?versionId="+vid1, ""), store, "bucket")
	if err != nil || !strings.Contains(resp, "HTTP/1.1 204 No Content") {
		t.Fatalf("permanent delete: resp=%s err=%v", resp, err)
	}
	if _, err := vs.GetVersion("bucket", "docs/file.txt", vid1); err == nil {
		t.Fatal("v1 should be permanently deleted")
	}
	rec2, ok2, lerr2 := vs.LatestVersion("bucket", "docs/file.txt")
	if lerr2 != nil || !ok2 || rec2.Hash != h2 {
		t.Fatalf("v2 must remain latest after v1 delete: %+v ok=%v err=%v", rec2, ok2, lerr2)
	}
}

func TestS3Versioning_UnversionedDeleteUnchanged(t *testing.T) {
	store, _ := newVersionedS3Stack(t)

	// No versioning configured: DELETE falls through to plain semantics.
	if err := store.Put("plain.txt", strings.Repeat("c", 64), 5, "", strings.NewReader("abcde")); err != nil {
		t.Fatal(err)
	}
	resp, err := runS3RequestCaptureBucket(t, s3Req("DELETE", "/bucket/plain.txt", ""), store, "bucket")
	if err != nil && err != ErrRequestHandled {
		t.Fatalf("plain delete: %v", err)
	}
	if strings.Contains(resp, "x-amz-delete-marker") {
		t.Fatalf("unversioned bucket must not create markers: %s", resp)
	}
	if _, _, err := store.Get("plain.txt"); err == nil {
		t.Fatal("plain delete should have removed the object")
	}
}

func TestS3Versioning_DeleteVersionIdWithoutVersioningStore(t *testing.T) {
	// mockStore has no versioning capability: DELETE ?versionId must 501
	// instead of falling through to a whole-object delete.
	resp, _ := runS3RequestCaptureBucket(t, s3Req("DELETE", "/bucket/f.txt?versionId=1", ""), &mockStore{}, "bucket")
	if !strings.Contains(resp, "<Code>NotImplemented</Code>") {
		t.Fatalf("expected 501 NotImplemented for versionId delete without versioning support:\n%s", resp)
	}
}

// ─── Handler coverage: happy + error branches per versioned route ───

func TestS3Versioning_GetVersioningConfigured(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	vs := store.(VersioningStore)
	if err := vs.SetBucketVersioning("bucket", storage.VersioningEnabled); err != nil {
		t.Fatal(err)
	}

	// Configured bucket echoes the status.
	resp, _ := runS3RequestCaptureBucket(t, s3Req("GET", "/bucket?versioning", ""), store, "bucket")
	if !strings.Contains(resp, "<Status>Enabled</Status>") || !strings.Contains(resp, "HTTP/1.1 200 OK") {
		t.Fatalf("GET versioning (enabled):\n%s", resp)
	}

	// Unconfigured bucket (fresh store): 200 with no Status element.
	fresh, _ := newVersionedS3Stack(t)
	resp, _ = runS3RequestCaptureBucket(t, s3Req("GET", "/bucket?versioning", ""), fresh, "bucket")
	if strings.Contains(resp, "<Status>") {
		t.Fatalf("unconfigured bucket must omit Status:\n%s", resp)
	}
	if !strings.Contains(resp, "<VersioningConfiguration") {
		t.Fatalf("expected VersioningConfiguration document:\n%s", resp)
	}
}

func TestS3Versioning_PutVersioningMalformed(t *testing.T) {
	store, _ := newVersionedS3Stack(t)

	// Malformed XML → 400 MalformedXML.
	resp, _ := runS3RequestCaptureBucket(t, s3Req("PUT", "/bucket?versioning",
		`<VersioningConfiguration><Status>`), store, "bucket")
	if !strings.Contains(resp, "<Code>MalformedXML</Code>") {
		t.Fatalf("expected MalformedXML:\n%s", resp)
	}

	// Well-formed XML with an invalid status → 400 MalformedXML.
	resp, _ = runS3RequestCaptureBucket(t, s3Req("PUT", "/bucket?versioning",
		`<VersioningConfiguration><Status>Bogus</Status></VersioningConfiguration>`), store, "bucket")
	if !strings.Contains(resp, "<Code>MalformedXML</Code>") {
		t.Fatalf("expected MalformedXML for invalid status:\n%s", resp)
	}

	// Missing Status element → 400 MalformedXML.
	resp, _ = runS3RequestCaptureBucket(t, s3Req("PUT", "/bucket?versioning",
		`<VersioningConfiguration></VersioningConfiguration>`), store, "bucket")
	if !strings.Contains(resp, "<Code>MalformedXML</Code>") {
		t.Fatalf("expected MalformedXML for missing status:\n%s", resp)
	}
}

func TestS3Versioning_GetVersionsPaginationAndClamp(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	vs := store.(VersioningStore)
	if err := vs.SetBucketVersioning("bucket", storage.VersioningEnabled); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := vs.RecordVersion("bucket", "k"+itoa(i), strings.Repeat(string(rune('a'+i)), 64), 1); err != nil {
			t.Fatal(err)
		}
	}

	// max-keys=2 → truncated page with next markers.
	resp, _ := runS3RequestCaptureBucket(t, s3Req("GET", "/bucket?versions&max-keys=2", ""), store, "bucket")
	if !strings.Contains(resp, "<IsTruncated>true</IsTruncated>") ||
		!strings.Contains(resp, "<NextKeyMarker>") || !strings.Contains(resp, "<NextVersionIdMarker>") {
		t.Fatalf("expected truncated page:\n%s", resp)
	}

	// max-keys=5000 clamps to 1000 (DoS guard).
	resp, _ = runS3RequestCaptureBucket(t, s3Req("GET", "/bucket?versions&max-keys=5000", ""), store, "bucket")
	if !strings.Contains(resp, "<MaxKeys>1000</MaxKeys>") {
		t.Fatalf("max-keys must clamp to 1000:\n%s", resp)
	}

	// prefix filter.
	resp, _ = runS3RequestCaptureBucket(t, s3Req("GET", "/bucket?versions&prefix=k0", ""), store, "bucket")
	if strings.Count(resp, "<VersionId>") != 1 {
		t.Fatalf("prefix filter must yield one version:\n%s", resp)
	}
}

func TestS3Versioning_GetVersionIdMissing(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	vs := store.(VersioningStore)
	if err := vs.SetBucketVersioning("bucket", storage.VersioningEnabled); err != nil {
		t.Fatal(err)
	}

	resp, _ := runS3RequestCaptureBucket(t, s3Req("GET", "/bucket/f.txt?versionId=no-such", ""), store, "bucket")
	if !strings.Contains(resp, "<Code>NoSuchVersion</Code>") {
		t.Fatalf("expected NoSuchVersion:\n%s", resp)
	}

	// HEAD variant: 404 without a body.
	resp, _ = runS3RequestCaptureBucket(t, s3Req("HEAD", "/bucket/f.txt?versionId=no-such", ""), store, "bucket")
	if !strings.Contains(resp, "HTTP/1.1 404 Not Found") {
		t.Fatalf("HEAD missing version must 404:\n%s", resp)
	}
}

func TestS3Versioning_HeadVersionId(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	vs := store.(VersioningStore)
	if err := vs.SetBucketVersioning("bucket", storage.VersioningEnabled); err != nil {
		t.Fatal(err)
	}
	h := strings.Repeat("e", 64)
	if err := store.Put("doc", h, 4, "", strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	vid, err := vs.RecordVersion("bucket", "doc", h, 4)
	if err != nil {
		t.Fatal(err)
	}

	resp, _ := runS3RequestCaptureBucket(t, s3Req("HEAD", "/bucket/doc?versionId="+vid, ""), store, "bucket")
	if !strings.Contains(resp, "HTTP/1.1 200 OK") ||
		!strings.Contains(resp, "x-amz-version-id: "+vid) ||
		!strings.Contains(resp, "Content-Length: 4\r\nConnection: close\r\n\r\n") {
		t.Fatalf("HEAD versionId response wrong:\n%s", resp)
	}
	// HEAD must carry no body.
	if strings.HasSuffix(strings.TrimSuffix(resp, "\r\n"), "data") {
		t.Fatalf("HEAD must not stream a body:\n%s", resp)
	}
}

func TestS3Versioning_PermanentDeleteMissing(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	vs := store.(VersioningStore)
	if err := vs.SetBucketVersioning("bucket", storage.VersioningEnabled); err != nil {
		t.Fatal(err)
	}
	resp, _ := runS3RequestCaptureBucket(t, s3Req("DELETE", "/bucket/f.txt?versionId=no-such", ""), store, "bucket")
	if !strings.Contains(resp, "<Code>NoSuchVersion</Code>") {
		t.Fatalf("expected NoSuchVersion:\n%s", resp)
	}
}

func TestS3Versioning_GetVersionIdWithoutVersioningStore(t *testing.T) {
	// GET ?versionId with a store lacking the capability → 501, not a plain Get.
	resp, _ := runS3RequestCaptureBucket(t, s3Req("GET", "/bucket/f.txt?versionId=1", ""), &mockStore{}, "bucket")
	if !strings.Contains(resp, "<Code>NotImplemented</Code>") {
		t.Fatalf("expected 501 for versionId GET without versioning support:\n%s", resp)
	}
}

func TestS3Versioning_MultipartCompletionEchoesVersion(t *testing.T) {
	store, _ := newVersionedS3Stack(t)
	vs := store.(VersioningStore)
	if err := vs.SetBucketVersioning("bucket", storage.VersioningEnabled); err != nil {
		t.Fatal(err)
	}
	// Seed one version so the multipart-completion path has state to verify.
	if err := store.Put("mp", strings.Repeat("f", 64), 1, "", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := vs.RecordVersion("bucket", "mp", strings.Repeat("f", 64), 1); err != nil {
		t.Fatal(err)
	}
	// Record another directly and assert the store keeps a coherent chain.
	page, err := vs.ListVersions("bucket", "", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(page.Records))
	}
	if !page.Records[0].IsLatest || page.Records[0].Key != "mp" {
		t.Fatalf("record fields wrong: %+v", page.Records[0])
	}
}
