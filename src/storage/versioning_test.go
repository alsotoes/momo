package storage

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"regexp"
	"strings"
	"syscall"
	"testing"
)

func TestBucketVersioningLifecycle(t *testing.T) {
	s := newTestStore(t)

	if _, configured, err := s.BucketVersioning("b1"); err != nil || configured {
		t.Fatalf("expected unconfigured bucket, configured=%v err=%v", configured, err)
	}
	if enabled, err := s.IsVersioned("b1"); err != nil || enabled {
		t.Fatalf("expected not versioned, enabled=%v err=%v", enabled, err)
	}

	if err := s.SetBucketVersioning("b1", VersioningEnabled); err != nil {
		t.Fatalf("SetBucketVersioning: %v", err)
	}
	status, configured, err := s.BucketVersioning("b1")
	if err != nil || !configured || status != VersioningEnabled {
		t.Fatalf("expected Enabled, got status=%q configured=%v err=%v", status, configured, err)
	}
	if enabled, err := s.IsVersioned("b1"); err != nil || !enabled {
		t.Fatalf("expected versioned, enabled=%v err=%v", enabled, err)
	}

	// Suspend keeps it configured but not enabled.
	if err := s.SetBucketVersioning("b1", VersioningSuspended); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	if enabled, err := s.IsVersioned("b1"); err != nil || enabled {
		t.Fatalf("expected suspended bucket not enabled, enabled=%v err=%v", enabled, err)
	}

	// Invalid status is rejected.
	if err := s.SetBucketVersioning("b1", "Bogus"); err == nil {
		t.Fatal("expected invalid status to be rejected")
	} else if !strings.Contains(err.Error(), "invalid versioning status") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRecordVersionAndLatest(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetBucketVersioning("b", VersioningEnabled); err != nil {
		t.Fatal(err)
	}

	v1, err := s.RecordVersion("b", "k1", "hash-a", 3)
	if err != nil {
		t.Fatalf("RecordVersion: %v", err)
	}
	v2, err := s.RecordVersion("b", "k1", "hash-b", 5)
	if err != nil {
		t.Fatalf("RecordVersion: %v", err)
	}
	if v1 == v2 {
		t.Fatal("expected distinct version ids per PUT")
	}

	rec, ok, err := s.LatestVersion("b", "k1")
	if err != nil || !ok {
		t.Fatalf("LatestVersion: ok=%v err=%v", ok, err)
	}
	if rec.VersionID != v2 || rec.Hash != "hash-b" || !rec.IsLatest {
		t.Fatalf("expected v2 latest, got %+v", rec)
	}

	// v1 must no longer be latest.
	rec1, err := s.GetVersion("b", "k1", v1)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if rec1.IsLatest {
		t.Fatalf("v1 should not be latest after v2: %+v", rec1)
	}
}

func TestDeleteMarkerSemantics(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetBucketVersioning("b", VersioningEnabled); err != nil {
		t.Fatal(err)
	}
	v1, err := s.RecordVersion("b", "k", "hash-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.RecordVersion("b", "k", "hash-2", 20)
	if err != nil {
		t.Fatal(err)
	}

	// Create delete marker — becomes latest, shadows the object.
	marker, err := s.RecordDeleteMarker("b", "k")
	if err != nil {
		t.Fatal(err)
	}
	rec, ok, err := s.LatestVersion("b", "k")
	if err != nil || !ok || !rec.IsDeleteMarker || !rec.IsLatest {
		t.Fatalf("expected delete marker latest, got %+v ok=%v err=%v", rec, ok, err)
	}

	// Removing the marker revives the previous data version (S3 semantics).
	deleted, err := s.DeleteVersion("b", "k", marker)
	if err != nil {
		t.Fatalf("DeleteVersion(marker): %v", err)
	}
	if !deleted.IsDeleteMarker {
		t.Fatalf("expected deleted record to be the marker: %+v", deleted)
	}
	rec, ok, err = s.LatestVersion("b", "k")
	if err != nil || !ok {
		t.Fatalf("LatestVersion after revive: ok=%v err=%v", ok, err)
	}
	if rec.VersionID != v2 || rec.IsDeleteMarker {
		t.Fatalf("expected v2 revived as latest, got %+v", rec)
	}

	// Permanently delete both data versions — key becomes empty.
	for _, vid := range []string{v1, v2} {
		if _, err := s.DeleteVersion("b", "k", vid); err != nil {
			t.Fatalf("DeleteVersion(%s): %v", vid, err)
		}
	}
	if _, ok, err := s.LatestVersion("b", "k"); err != nil || ok {
		t.Fatalf("expected no versions after purge, ok=%v err=%v", ok, err)
	}
}

func TestListVersionsOrderingAndPagination(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetBucketVersioning("b", VersioningEnabled); err != nil {
		t.Fatal(err)
	}
	// Two keys, versions interleaved.
	want := map[string][]string{
		"a-key": {"h1", "h2", "h3"},
		"b-key": {"h4", "h5"},
	}
	for key, hashes := range want {
		for _, h := range hashes {
			if _, err := s.RecordVersion("b", key, h, 1); err != nil {
				t.Fatalf("RecordVersion(%s): %v", key, err)
			}
		}
	}
	// A delete marker on a-key shadows nothing for listing — it must appear.
	if _, err := s.RecordDeleteMarker("b", "a-key"); err != nil {
		t.Fatal(err)
	}

	// Full listing: keys asc, lastmod desc within key.
	page, err := s.ListVersions("b", "", "", "", 0)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if page.IsTruncated {
		t.Fatal("unlimited page must not be truncated")
	}
	// a-key: marker, h3, h2, h1 (newest first); then b-key: h5, h4.
	expectOrder := []struct {
		key, hash string
		marker    bool
	}{
		{"a-key", "", true}, {"a-key", "h3", false}, {"a-key", "h2", false}, {"a-key", "h1", false},
		{"b-key", "h5", false}, {"b-key", "h4", false},
	}
	if len(page.Records) != len(expectOrder) {
		t.Fatalf("expected %d records, got %d", len(expectOrder), len(page.Records))
	}
	// Records don't carry keys; validate via order of markers/hashes per key run.
	if !page.Records[0].IsDeleteMarker {
		t.Fatalf("expected first record to be a-key's marker: %+v", page.Records[0])
	}
	gotA := []string{page.Records[1].Hash, page.Records[2].Hash, page.Records[3].Hash}
	if gotA[0] != "h3" || gotA[1] != "h2" || gotA[2] != "h1" {
		t.Fatalf("a-key order wrong: %v", gotA)
	}
	gotB := []string{page.Records[4].Hash, page.Records[5].Hash}
	if gotB[0] != "h5" || gotB[1] != "h4" {
		t.Fatalf("b-key order wrong: %v", gotB)
	}

	// Pagination: maxKeys=2 → first page h3-page, truncated with markers.
	p1, err := s.ListVersions("b", "", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !p1.IsTruncated || len(p1.Records) != 2 || p1.NextKeyMarker == "" || p1.NextVersionID == "" {
		t.Fatalf("page1 wrong: %+v", p1)
	}

	// Resume from the marker: must return exactly the remaining records.
	p2, err := s.ListVersions("b", "", p1.NextKeyMarker, p1.NextVersionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.Records)+len(p2.Records) != len(expectOrder) {
		t.Fatalf("pagination lost records: %d + %d != %d", len(p1.Records), len(p2.Records), len(expectOrder))
	}
	// No duplicates across pages.
	seen := map[string]bool{}
	for _, r := range append(append([]VersionRecord{}, p1.Records...), p2.Records...) {
		if seen[r.VersionID] {
			t.Fatalf("duplicate version across pages: %s", r.VersionID)
		}
		seen[r.VersionID] = true
	}
}

func TestListVersionsPrefixFilter(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetBucketVersioning("b", VersioningEnabled); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"logs/one", "logs/two", "data/one"} {
		if _, err := s.RecordVersion("b", key, "hash-"+key, 1); err != nil {
			t.Fatal(err)
		}
	}

	page, err := s.ListVersions("b", "logs/", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 2 {
		t.Fatalf("expected 2 records under logs/, got %d", len(page.Records))
	}
}

func TestVersionErrors(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetBucketVersioning("b", VersioningEnabled); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetVersion("b", "missing", "no-such-id"); err != syscall.ENOENT {
		t.Fatalf("expected ENOENT, got %v", err)
	}
	if _, err := s.DeleteVersion("b", "missing", "no-such-id"); err != syscall.ENOENT {
		t.Fatalf("expected ENOENT, got %v", err)
	}
	if _, _, err := s.GetVersionContent("b", "missing", "no-such-id"); err != syscall.ENOENT {
		t.Fatalf("expected ENOENT, got %v", err)
	}
}

func TestGetVersionContent(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetBucketVersioning("b", VersioningEnabled); err != nil {
		t.Fatal(err)
	}
	// Store real content so the version's blob resolves.
	payload := []byte("versioned payload")
	hash := fmt.Sprintf("%x", sha256Sum(payload))
	if err := s.Put("obj", hash, int64(len(payload)), "", bytes.NewReader(payload)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	vid, err := s.RecordVersion("b", "obj", hash, int64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}

	rc, rec, err := s.GetVersionContent("b", "obj", vid)
	if err != nil {
		t.Fatalf("GetVersionContent: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("content mismatch: %q", data)
	}
	if rec.Hash != hash {
		t.Fatalf("record hash mismatch: %s", rec.Hash)
	}

	// Delete markers have no content.
	marker, err := s.RecordDeleteMarker("b", "obj")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetVersionContent("b", "obj", marker); err != syscall.ENOENT {
		t.Fatalf("expected ENOENT for marker content, got %v", err)
	}
}

func TestVersionIDFormat(t *testing.T) {
	// UUIDv4 shape: 8-4-4-4-12 hex with version/variant nibbles.
	shape := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := newVersionID()
		if err != nil {
			t.Fatalf("newVersionID: %v", err)
		}
		if !shape.MatchString(id) {
			t.Fatalf("bad version id shape: %s", id)
		}
		if seen[id] {
			t.Fatalf("duplicate version id: %s", id)
		}
		seen[id] = true
	}
}

func TestRepointNamespace(t *testing.T) {
	s := newTestStore(t)

	if err := s.RepointNamespace("name", "hash-a"); err != nil {
		t.Fatalf("RepointNamespace: %v", err)
	}
	if h, err := s.GetHashForName("name"); err != nil || h != "hash-a" {
		t.Fatalf("expected hash-a, got %q err=%v", h, err)
	}
	if err := s.RepointNamespace("name", "hash-b"); err != nil {
		t.Fatal(err)
	}
	if h, err := s.GetHashForName("name"); err != nil || h != "hash-b" {
		t.Fatalf("expected hash-b, got %q err=%v", h, err)
	}
	if err := s.RepointNamespace("name", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetHashForName("name"); err != syscall.ENOENT {
		t.Fatalf("expected ENOENT after removal, got %v", err)
	}
}

func sha256Sum(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}
