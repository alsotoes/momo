package transport

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedUpload registers a session created at the given time and returns its id.
func seedUpload(t *testing.T, id string, created time.Time, spillDir string) {
	t.Helper()
	muUploads.Lock()
	uploads[id] = &multipartUpload{bucket: "b", key: "k", createdAt: created, spillDir: spillDir}
	muUploads.Unlock()
	t.Cleanup(func() {
		muUploads.Lock()
		delete(uploads, id)
		muUploads.Unlock()
	})
}

// hasUpload reports whether a session still exists.
func hasUpload(id string) bool {
	muUploads.Lock()
	defer muUploads.Unlock()
	_, ok := uploads[id]
	return ok
}

// TestReaperDropsExpiredSession is the core guarantee of #1361: an abandoned
// upload stops holding disk once its TTL passes.
func TestReaperDropsExpiredSession(t *testing.T) {
	spill := t.TempDir()
	if err := os.WriteFile(filepath.Join(spill, "1.part"), []byte("payload"), 0o600); err != nil {
		t.Fatalf("write part: %v", err)
	}
	seedUpload(t, "expired-1", time.Now().Add(-48*time.Hour), spill)

	sessions, _ := ReapExpiredMultipartUploads(24*time.Hour, time.Now(), t.TempDir())

	if sessions != 1 {
		t.Fatalf("reaped %d sessions, want 1", sessions)
	}
	if hasUpload("expired-1") {
		t.Fatal("expired session survived the reap")
	}
	if _, err := os.Stat(spill); !os.IsNotExist(err) {
		t.Fatalf("spill directory survived the reap: %v", err)
	}
}

// TestReaperKeepsFreshSession proves an in-flight upload is never destroyed.
func TestReaperKeepsFreshSession(t *testing.T) {
	spill := t.TempDir()
	seedUpload(t, "fresh-1", time.Now(), spill)

	sessions, _ := ReapExpiredMultipartUploads(24*time.Hour, time.Now(), t.TempDir())

	if sessions != 0 {
		t.Fatalf("reaped %d sessions, want 0", sessions)
	}
	if !hasUpload("fresh-1") {
		t.Fatal("fresh session was reaped")
	}
	if _, err := os.Stat(spill); err != nil {
		t.Fatalf("fresh spill directory removed: %v", err)
	}
}

// TestReaperDisabled proves ttl=0 turns the policy off completely.
func TestReaperDisabled(t *testing.T) {
	seedUpload(t, "kept-forever", time.Now().Add(-365*24*time.Hour), "")

	sessions, orphans := ReapExpiredMultipartUploads(0, time.Now(), t.TempDir())
	if sessions != 0 || orphans != 0 {
		t.Fatalf("disabled reaper removed %d/%d", sessions, orphans)
	}
	if !hasUpload("kept-forever") {
		t.Fatal("disabled reaper dropped a session")
	}
}

// TestReaperRemovesOrphanSpill covers the daemon-killed-mid-upload case: a
// directory with no session behind it is cleaned up once it is old enough.
func TestReaperRemovesOrphanSpill(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, multipartSpillPrefix+"orphan-old")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(old, "1.part"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	oldTime := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	recent := filepath.Join(root, multipartSpillPrefix+"orphan-recent")
	if err := os.MkdirAll(recent, 0o700); err != nil {
		t.Fatalf("mkdir recent: %v", err)
	}

	unrelated := filepath.Join(root, "some-other-dir")
	if err := os.MkdirAll(unrelated, 0o700); err != nil {
		t.Fatalf("mkdir unrelated: %v", err)
	}
	if err := os.Chtimes(unrelated, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes unrelated: %v", err)
	}

	_, orphans := ReapExpiredMultipartUploads(24*time.Hour, time.Now(), root)
	if orphans != 1 {
		t.Fatalf("removed %d orphans, want 1", orphans)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old orphan survived: %v", err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("recent orphan was removed: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated directory was removed: %v", err)
	}
}

// TestReaperKeepsSpillOfLiveSession proves the orphan sweep never deletes a
// directory a live session still owns, even when it looks old.
func TestReaperKeepsSpillOfLiveSession(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, multipartSpillPrefix+"live")
	if err := os.MkdirAll(live, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	oldTime := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(live, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	seedUpload(t, "live-1", time.Now(), live)

	_, orphans := ReapExpiredMultipartUploads(24*time.Hour, time.Now(), root)
	if orphans != 0 {
		t.Fatalf("removed %d orphans, want 0 (owned by a live session)", orphans)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("live session spill removed: %v", err)
	}
}

// TestStartMultipartReaperSweepsAndStops proves the loop reaps at startup and
// exits when the context is cancelled.
func TestStartMultipartReaperSweepsAndStops(t *testing.T) {
	seedUpload(t, "startup-expired", time.Now().Add(-48*time.Hour), "")

	ctx, cancel := context.WithCancel(context.Background())
	done := StartMultipartReaper(ctx, time.Hour)

	deadline := time.After(5 * time.Second)
	for hasUpload("startup-expired") {
		select {
		case <-deadline:
			t.Fatal("startup sweep did not reap the expired session")
		case <-time.After(20 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reaper did not stop on cancellation")
	}
}

// TestStartMultipartReaperDisabledOnZeroTTL proves a zero TTL starts no loop.
func TestStartMultipartReaperDisabledOnZeroTTL(t *testing.T) {
	done := StartMultipartReaper(context.Background(), 0)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("disabled reaper did not return immediately")
	}
}

// TestMultipartTTLFromConfig covers the policy parser, including the fallback.
func TestMultipartTTLFromConfig(t *testing.T) {
	cases := map[string]time.Duration{
		"":      24 * time.Hour,
		"72h":   72 * time.Hour,
		"30m":   30 * time.Minute,
		"0":     0,
		"bogus": 24 * time.Hour,
	}
	for raw, want := range cases {
		if got := MultipartTTLFromConfig(raw); got != want {
			t.Fatalf("MultipartTTLFromConfig(%q) = %s, want %s", raw, got, want)
		}
	}
}
