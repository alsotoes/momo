package transport

import (
	"io"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// taggingStack boots an s3-tcp gateway over a mock store that keeps S3 metadata.
func taggingStack(t *testing.T) (addr, auth string) {
	t.Helper()
	auth = "tagging-token-111111111111111111111111111111111111111111111111" // notsecret
	cfg := common.Configuration{Global: common.ConfigurationGlobal{
		Protocol: "s3-tcp", AuthToken: auth, TLSInsecure: true,
	}}
	factory := NewProtocolFactory(cfg)
	l, err := factory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	store := &tagStore{meta: map[string]map[string]string{}}
	store.putFunc = func(string, string, int64, string, io.Reader) error { return nil }
	startServer(t, l, auth, store, 8)
	return l.Addr().String(), auth
}

// TestObjectTaggingRoundTrip proves PUT/GET/DELETE over a real socket.
func TestObjectTaggingRoundTrip(t *testing.T) {
	t.Cleanup(func() { verifyNoLeaks(t) })
	addr, auth := taggingStack(t)

	put := taggingRequest(t, "PUT", addr, auth, "obj.txt",
		`<Tagging><TagSet><Tag><Key>team</Key><Value>storage</Value></Tag></TagSet></Tagging>`)
	if !strings.Contains(put, "200") {
		t.Fatalf("PUT tagging failed: %s", truncate(put, 240))
	}

	got := taggingRequest(t, "GET", addr, auth, "obj.txt", "")
	if !strings.Contains(got, "<Key>team</Key>") || !strings.Contains(got, "<Value>storage</Value>") {
		t.Fatalf("GET tagging did not return the tag: %s", truncate(got, 300))
	}
	if !strings.Contains(got, "<TagSet>") || !strings.Contains(got, "xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"") {
		t.Fatalf("GET tagging shape is wrong: %s", truncate(got, 300))
	}

	del := taggingRequest(t, "DELETE", addr, auth, "obj.txt", "")
	if !strings.Contains(del, "204") {
		t.Fatalf("DELETE tagging failed: %s", truncate(del, 240))
	}
	empty := taggingRequest(t, "GET", addr, auth, "obj.txt", "")
	if !strings.Contains(empty, "<TagSet></TagSet>") {
		t.Fatalf("tags survived the delete: %s", truncate(empty, 300))
	}
}

// TestObjectTaggingValidation pins the S3 tag rules through the HTTP path.
func TestObjectTaggingValidation(t *testing.T) {
	t.Cleanup(func() { verifyNoLeaks(t) })
	addr, auth := taggingStack(t)

	eleven := &strings.Builder{}
	eleven.WriteString("<Tagging><TagSet>")
	for i := 0; i < 11; i++ {
		eleven.WriteString("<Tag><Key>k" + string(rune('a'+i)) + "</Key><Value>v</Value></Tag>")
	}
	eleven.WriteString("</TagSet></Tagging>")

	cases := map[string]string{
		"eleven tags":   eleven.String(),
		"empty key":     `<Tagging><TagSet><Tag><Key></Key><Value>v</Value></Tag></TagSet></Tagging>`,
		"duplicate key": `<Tagging><TagSet><Tag><Key>k</Key><Value>1</Value></Tag><Tag><Key>k</Key><Value>2</Value></Tag></TagSet></Tagging>`,
		"long key":      `<Tagging><TagSet><Tag><Key>` + strings.Repeat("k", 129) + `</Key><Value>v</Value></Tag></TagSet></Tagging>`,
		"long value":    `<Tagging><TagSet><Tag><Key>k</Key><Value>` + strings.Repeat("v", 257) + `</Value></Tag></TagSet></Tagging>`,
		"not xml":       `<Tagging><TagSet>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp := taggingRequest(t, "PUT", addr, auth, "obj.txt", body)
			if !strings.Contains(resp, "400") {
				t.Fatalf("expected 400 for %s, got: %s", name, truncate(resp, 240))
			}
		})
	}
}

// TestObjectTaggingUnknownKey proves tagging a missing object is a 404, not an
// empty success.
func TestObjectTaggingUnknownKey(t *testing.T) {
	t.Cleanup(func() { verifyNoLeaks(t) })
	auth := "tagging-missing-1111111111111111111111111111111111111111111111" // notsecret
	cfg := common.Configuration{Global: common.ConfigurationGlobal{
		Protocol: "s3-tcp", AuthToken: auth, TLSInsecure: true,
	}}
	factory := NewProtocolFactory(cfg)
	l, err := factory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	store := &tagStore{meta: map[string]map[string]string{}, missing: true}
	startServer(t, l, auth, store, 4)

	resp := taggingRequest(t, "GET", l.Addr().String(), auth, "missing.txt", "")
	if !strings.Contains(resp, "NoSuchKey") {
		t.Fatalf("expected NoSuchKey, got: %s", truncate(resp, 240))
	}
}

// TestTagSetEncodingIsDeterministicAndReversible proves the stored wire form sorts,
// escapes and round-trips (it is what replication carries).
func TestTagSetEncodingIsDeterministicAndReversible(t *testing.T) {
	tags := tagSet{
		{Key: "zebra", Value: "a b&c"},
		{Key: "alpha", Value: "1=2"},
	}
	encoded := tags.encode()
	if !strings.HasPrefix(encoded, "alpha=") {
		t.Fatalf("encoding is not key-sorted: %q", encoded)
	}
	back, err := decodeTags(encoded)
	if err != nil {
		t.Fatalf("decodeTags: %v", err)
	}
	if len(back) != 2 || back[0].Key != "alpha" || back[0].Value != "1=2" {
		t.Fatalf("round-trip = %#v", back)
	}

	if _, derr := decodeTags("noequals"); derr == nil {
		t.Fatal("an unparsable tag list must be refused")
	}
	empty, eerr := decodeTags("")
	if eerr != nil || len(empty) != 0 {
		t.Fatalf("empty encoding = %#v err=%v", empty, eerr)
	}
}

// taggingRequest performs one tagging request against addr.
func taggingRequest(t *testing.T, method, addr, auth, key, body string) string {
	t.Helper()
	now := time.Now().UTC().Format("20060102T150405Z")
	req := method + " /bucket/" + key + "?tagging HTTP/1.1\r\n" +
		"Host: " + addr + "\r\nX-Amz-Date: " + now + "\r\n" +
		"Authorization: Bearer " + auth + "\r\n" +
		"Content-Type: application/xml\r\nContent-Length: " + itoaTest(len(body)) + "\r\n\r\n" + body
	return doS3Request(t, addr, req)
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

var _ = http.MethodPut

// tagStore is a store double whose S3 metadata survives a put/get cycle, so the
// tagging handler's write can be read back.
type tagStore struct {
	mockStore
	meta    map[string]map[string]string
	missing bool
}

// GetEOF models a missing object when the test asks for one.
func (s *tagStore) Get(name string) (io.ReadCloser, common.FileMetadata, error) {
	if s.missing {
		return nil, common.FileMetadata{}, syscall.ENOENT
	}
	return io.NopCloser(strings.NewReader("payload")), common.FileMetadata{Name: name, Hash: "h", Size: 7}, nil
}

func (s *tagStore) GetS3Meta(key string) map[string]string { return s.meta[key] }

func (s *tagStore) PutS3Meta(key string, headers map[string]string) error {
	s.meta[key] = headers
	return nil
}

// TestUploadCarriesTaggingHeader proves x-amz-tagging on a plain PUT becomes the
// object's tag set (issue #1375), in canonical key-sorted form.
func TestUploadCarriesTaggingHeader(t *testing.T) {
	comm, client := newPipeComm(t)
	go drainFor(client)

	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1/bucket/tagged.txt", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	// Deliberately unsorted and percent-encoded, as a client would send it.
	req.Header.Set("X-Amz-Tagging", "zebra=a%20b&alpha=1")

	if verr := comm.validateTaggingHeader(req); verr != nil {
		t.Fatalf("validateTaggingHeader: %v", verr)
	}
	if got := req.Header.Get("X-Amz-Tagging"); got != "alpha=1&zebra=a+b" && got != "alpha=1&zebra=a%20b" {
		t.Fatalf("header was not canonicalised, got %q", got)
	}
	// The header is what collectS3Headers persists with the object metadata.
	headers := collectS3Headers(req)
	if headers[s3TaggingHeader] == "" {
		t.Fatalf("tagging header was not collected for persistence: %#v", headers)
	}
	tags, derr := decodeTags(headers[s3TaggingHeader])
	if derr != nil || len(tags) != 2 {
		t.Fatalf("stored tags = %#v err=%v", tags, derr)
	}
}

// TestUploadRejectsInvalidTaggingHeader proves a bad tag header fails the upload
// before any payload is stored.
func TestUploadRejectsInvalidTaggingHeader(t *testing.T) {
	comm, client := newPipeComm(t)

	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1/bucket/badtags.txt", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("X-Amz-Tagging", "dup=1&dup=2")

	answer := make(chan string, 1)
	go func() {
		_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
		data, _ := io.ReadAll(client)
		answer <- string(data)
	}()

	if verr := comm.validateTaggingHeader(req); verr == nil {
		t.Fatal("a duplicate tag key must be refused")
	}
	if got := <-answer; !strings.Contains(got, "InvalidTag") {
		t.Fatalf("expected an InvalidTag answer, got %q", got)
	}
}

// tagStore is a store double whose S3 metadata survives a put/get cycle, so the
// tagging handler's write can be read back.
