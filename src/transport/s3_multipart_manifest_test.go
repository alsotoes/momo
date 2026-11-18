package transport

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// resetSessions clears the package session map for the duration of a test, so
// recovery can be observed from a known-empty state (a "restart").
func resetSessions(t *testing.T) {
	t.Helper()
	muUploads.Lock()
	saved := uploads
	uploads = make(map[string]*multipartUpload)
	muUploads.Unlock()
	t.Cleanup(func() {
		muUploads.Lock()
		uploads = saved
		muUploads.Unlock()
	})
}

// useSpillRoot points the spill root at a fresh directory for the test.
func useSpillRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	SetMultipartSpillRoot(root)
	t.Cleanup(func() { SetMultipartSpillRoot("") })
	return filepath.Join(root, "multipart-spill")
}

// openSession creates a session with one spooled part and returns its id.
func openSession(t *testing.T, id string, createdAt time.Time, parts ...int64) *multipartUpload {
	t.Helper()
	up := &multipartUpload{bucket: "bucket", key: "big.bin", createdAt: createdAt}
	dir, err := createMultipartSpillDir()
	if err != nil {
		t.Fatalf("createMultipartSpillDir: %v", err)
	}
	up.spillDir = dir
	for i, size := range parts {
		path := filepath.Join(dir, itoaForTest(i+1)+".part")
		if werr := os.WriteFile(path, make([]byte, size), 0o600); werr != nil {
			t.Fatalf("write part: %v", werr)
		}
		up.parts = append(up.parts, multipartPart{
			partNumber: i + 1, etag: "etag", size: size, path: path,
		})
	}
	muUploads.Lock()
	uploads[id] = up
	muUploads.Unlock()
	writeManifest(id, up)
	return up
}

func itoaForTest(n int) string {
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

// TestManifestSurvivesRestart is the core guarantee of #1371: the session comes
// back with its id and parts after the process state is gone.
func TestManifestSurvivesRestart(t *testing.T) {
	useSpillRoot(t)
	resetSessions(t)

	created := time.Now().Add(-time.Hour)
	original := openSession(t, "recover-me", created, 10, 20)

	// Simulate a restart: forget every session, keep the disk.
	muUploads.Lock()
	uploads = make(map[string]*multipartUpload)
	muUploads.Unlock()

	if got := RecoverMultipartSessions(24*time.Hour, time.Now()); got != 1 {
		t.Fatalf("recovered %d sessions, want 1", got)
	}

	muUploads.Lock()
	up, ok := uploads["recover-me"]
	muUploads.Unlock()
	if !ok {
		t.Fatal("upload id did not survive the restart")
	}
	if up.bucket != original.bucket || up.key != original.key {
		t.Fatalf("recovered (%q,%q), want (%q,%q)", up.bucket, up.key, original.bucket, original.key)
	}
	if len(up.parts) != 2 {
		t.Fatalf("recovered %d parts, want 2", len(up.parts))
	}
	if up.parts[1].size != 20 || up.parts[1].path == "" {
		t.Fatalf("recovered part = %+v", up.parts[1])
	}
}

// TestManifestSkipsExpiredSession proves the TTL still governs: an expired upload
// is not recovered, so the reaper reclaims its disk.
func TestManifestSkipsExpiredSession(t *testing.T) {
	useSpillRoot(t)
	resetSessions(t)

	openSession(t, "too-old", time.Now().Add(-48*time.Hour), 5)
	muUploads.Lock()
	uploads = make(map[string]*multipartUpload)
	muUploads.Unlock()

	if got := RecoverMultipartSessions(24*time.Hour, time.Now()); got != 0 {
		t.Fatalf("recovered %d sessions, want 0 (expired)", got)
	}
}

// TestManifestSkipsMissingPart proves a manifest that claims bytes the disk does
// not have is refused, instead of resuming an upload that would complete wrong.
func TestManifestSkipsMissingPart(t *testing.T) {
	useSpillRoot(t)
	resetSessions(t)

	up := openSession(t, "truncated", time.Now().Add(-time.Minute), 8)
	if err := os.Remove(up.parts[0].path); err != nil {
		t.Fatalf("remove part: %v", err)
	}
	muUploads.Lock()
	uploads = make(map[string]*multipartUpload)
	muUploads.Unlock()

	if got := RecoverMultipartSessions(24*time.Hour, time.Now()); got != 0 {
		t.Fatalf("recovered %d sessions, want 0 (missing part)", got)
	}
}

// TestManifestSkipsMalformed proves an unreadable or incomplete manifest never
// crashes recovery.
func TestManifestSkipsMalformed(t *testing.T) {
	root := useSpillRoot(t)
	resetSessions(t)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	bad := filepath.Join(root, "garbage")
	if err := os.MkdirAll(bad, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bad, multipartManifestName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	incomplete := filepath.Join(root, "incomplete")
	if err := os.MkdirAll(incomplete, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"version":1,"upload_id":"x"}` // no bucket/key
	if err := os.WriteFile(filepath.Join(incomplete, multipartManifestName), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := RecoverMultipartSessions(24*time.Hour, time.Now()); got != 0 {
		t.Fatalf("recovered %d sessions, want 0", got)
	}
}

// TestManifestWriteIsAtomic proves the manifest is written via a rename, leaving
// no temporary file behind and never a partial body.
func TestManifestWriteIsAtomic(t *testing.T) {
	useSpillRoot(t)
	resetSessions(t)

	up := openSession(t, "atomic", time.Now(), 4)
	if _, err := os.Stat(filepath.Join(up.spillDir, multipartManifestName+".tmp")); !os.IsNotExist(err) {
		t.Fatalf("temporary manifest left behind: %v", err)
	}
	data, rerr := os.ReadFile(filepath.Join(up.spillDir, multipartManifestName))
	if rerr != nil {
		t.Fatalf("read manifest: %v", rerr)
	}
	var manifest multipartManifest
	if uerr := json.Unmarshal(data, &manifest); uerr != nil {
		t.Fatalf("manifest is not valid JSON: %v", uerr)
	}
	if manifest.UploadID != "atomic" || len(manifest.Parts) != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}
}

// TestRecoverWithUnusableSpillRoot proves recovery is a no-op (not a crash) when
// the spill root cannot be created — e.g. a file where the directory belongs.
func TestRecoverWithUnusableSpillRoot(t *testing.T) {
	resetSessions(t)
	root := t.TempDir()
	// A regular file at <data>/multipart-spill makes MkdirAll fail.
	if err := os.WriteFile(filepath.Join(root, "multipart-spill"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	SetMultipartSpillRoot(root)
	t.Cleanup(func() { SetMultipartSpillRoot("") })

	if got := RecoverMultipartSessions(time.Hour, time.Now()); got != 0 {
		t.Fatalf("recovered %d sessions with an unusable spill root", got)
	}
}

// TestEndToEndRecoveryCompletesUpload proves the whole point of #1371: an upload
// created, partly written, "restarted", and then completed produces the object.
func TestEndToEndRecoveryCompletesUpload(t *testing.T) {
	t.Cleanup(func() { verifyNoLeaks(t) })
	useSpillRoot(t)
	resetSessions(t)
	done := StartMultipartReaper(context.Background(), 0)
	<-done

	stack := newCopyStack(t, []byte("part-one-bytes"))
	id := stack.begin(t)
	if resp := stack.copyPart(t, id, 1, ""); !strings.Contains(resp, "200 OK") {
		t.Fatalf("copy part failed: %s", truncate(resp, 200))
	}

	// Restart: drop every session, then recover from the manifests on disk.
	muUploads.Lock()
	uploads = make(map[string]*multipartUpload)
	muUploads.Unlock()
	if got := RecoverMultipartSessions(24*time.Hour, time.Now()); got != 1 {
		t.Fatalf("recovered %d sessions, want 1", got)
	}

	if done := stack.complete(t, id, 1); !strings.Contains(done, "200 OK") {
		t.Fatalf("complete after recovery failed: %s", truncate(done, 240))
	}
	if string(stack.putData) != "part-one-bytes" {
		t.Fatalf("assembled %q", stack.putData)
	}
}
