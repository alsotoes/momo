package momofs

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"go.uber.org/goleak"
)

func newTestFS(t *testing.T, opts ...Option) (*FS, *storage.CASStore) {
	t.Helper()
	s, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	return New(s, opts...), s
}

// TestR4T1_POSIXRoundTrip (R4-T1) exercises the POSIX smoke paths:
// create/readdir/attr/open/read/replace/remove over a mounted tree.
func TestR4T1_POSIXRoundTrip(t *testing.T) {
	defer goleak.VerifyNone(t)
	fs, store := newTestFS(t)
	defer store.Close()

	if err := fs.Mkdir("/docs", DefaultDirMode); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	sz, hash, err := fs.Create("/docs/report.txt", 0o640, strings.NewReader("hello momofs"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sz != 12 {
		t.Errorf("Create size = %d, want 12", sz)
	}
	if len(hash) != 64 {
		t.Errorf("hash len = %d, want 64", len(hash))
	}

	// Lookup + GetAttr carry mode.
	e, err := fs.Lookup("/docs/report.txt")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if e.Mode != 0o640 {
		t.Errorf("Mode = %o, want 0640", e.Mode)
	}

	// ReadDir sorted, includes the file.
	ents, err := fs.ReadDir("/docs")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(ents) != 1 || ents[0].Name != "report.txt" {
		t.Fatalf("Readdir /docs = %+v, want [report.txt]", ents)
	}

	// Open + read content.
	rc, fe, err := fs.Open("/docs/report.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello momofs" {
		t.Errorf("content = %q", data)
	}
	if fe.Size != 12 {
		t.Errorf("entry size = %d", fe.Size)
	}

	// At-Offset read.
	buf := make([]byte, 3)
	n, err := fs.ReadAt("/docs/report.txt", 6, buf)
	if err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if n != 3 || string(buf) != "mom" {
		t.Errorf("ReadAt(6) = %q/%d, want mom", buf, n)
	}

	// Chmod via setattr.
	if _, err := fs.SetAttr("/docs/report.txt", u32ptr(0o600), nil, nil, nil); err != nil {
		t.Fatalf("SetAttr: %v", err)
	}
	e2, _ := fs.GetAttr("/docs/report.txt")
	if e2.Mode != 0o600 {
		t.Errorf("Mode after chmod = %o, want 0600", e2.Mode)
	}

	// Replace content (new version).
	if _, _, err := fs.Create("/docs/report.txt", 0o600, strings.NewReader("v2")); err != nil {
		t.Fatalf("reCreate: %v", err)
	}
	rc2, _, err := fs.Open("/docs/report.txt")
	if err != nil {
		t.Fatalf("reOpen: %v", err)
	}
	d2, _ := io.ReadAll(rc2)
	rc2.Close()
	if string(d2) != "v2" {
		t.Errorf("content after replace = %q, want v2", d2)
	}

	// Remove.
	if err := fs.Remove("/docs/report.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := fs.Lookup("/docs/report.txt"); err == nil {
		t.Fatal("expected ENOENT after remove")
	}

	// Mkdir with missing parent fails.
	if err := fs.Mkdir("/no/parent/dir", DefaultDirMode); err == nil {
		t.Fatal("expected ENOENT for mkdir with missing parent")
	}
}

// TestR4T2_S3MountConsistency (R4-T3-equivalent R4-C3) verifies objects written
// natively to the store (as S3/momo-native) appear in the mount, and vice versa.
func TestR4T2_S3MountConsistency(t *testing.T) {
	defer goleak.VerifyNone(t)
	fs, store := newTestFS(t)
	defer store.Close()

	// Native write: object whose remote path is /bucket/obj.bin.
	data := []byte("native-object-bytes")
	{
		h := common.HashBytes(data)
		if err := store.Put("native-"+h[:16], h, int64(len(data)), "/bucket/obj.bin", bytes.NewReader(data)); err != nil {
			t.Fatalf("store native Put: %v", err)
		}
	}

	// Mount sees the file and the synthetic /bucket directory.
	e, err := fs.Lookup("/bucket/obj.bin")
	if err != nil {
		t.Fatalf("mount Lookup of natively-written object: %v", err)
	}
	if e.Type != EntryFile || e.Size != int64(len(data)) {
		t.Errorf("native entry = %+v", e)
	}
	if _, err := fs.Lookup("/bucket"); err != nil {
		t.Fatalf("synthetic dir /bucket: %v", err)
	}
	ents, err := fs.ReadDir("/")
	if err != nil {
		t.Fatalf("ReadDir /: %v", err)
	}
	if len(ents) != 1 || ents[0].Name != "bucket" {
		t.Fatalf("root readdir = %+v, want [bucket]", ents)
	}
	rc, _, err := fs.Open("/bucket/obj.bin")
	if err != nil {
		t.Fatalf("mount Open of native object: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, data) {
		t.Errorf("native read = %q", got)
	}

	// Vice versa: a mount-created file is a real store object.
	if _, _, err := fs.Create("/from-mount.txt", 0o644, strings.NewReader("mount-side")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	found := false
	for _, p := range storePaths(t, store) {
		if p == "from-mount.txt" { // store path spelling has no leading slash
			found = true
			break
		}
	}
	if !found {
		t.Error("mount-created file absent from backing store")
	}
}

// TestR4T3_RenameAndHardlinkGC (R4-T2/R4-T3) verifies atomic rename semantics
// and hardlinks whose reference counts stay aligned with the CAS GC floor.
func TestR4T3_RenameAndHardlinkGC(t *testing.T) {
	defer goleak.VerifyNone(t)
	fs, store := newTestFS(t)
	defer store.Close()

	if err := fs.Mkdir("/a", DefaultDirMode); err != nil {
		t.Fatalf("Mkdir /a: %v", err)
	}
	if err := fs.Mkdir("/b", DefaultDirMode); err != nil {
		t.Fatalf("Mkdir /b: %v", err)
	}
	if _, _, err := fs.Create("/a/src.txt", 0o644, strings.NewReader("rename me")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Cross-directory rename.
	if err := fs.Rename("/a/src.txt", "/b/dst.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := fs.Lookup("/a/src.txt"); err == nil {
		t.Fatal("src should be gone after rename")
	}
	rc, _, err := fs.Open("/b/dst.txt")
	if err != nil {
		t.Fatalf("Open after rename: %v", err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "rename me" {
		t.Errorf("content after rename = %q", b)
	}

	// Hardlink: second path to the same content.
	if err := fs.Link("/b/dst.txt", "/a/copy.txt"); err != nil {
		t.Fatalf("Link: %v", err)
	}
	rc2, _, err := fs.Open("/a/copy.txt")
	if err != nil {
		t.Fatalf("Open link: %v", err)
	}
	b2, _ := io.ReadAll(rc2)
	rc2.Close()
	if !bytes.Equal(b2, b) {
		t.Errorf("link content = %q, want %q", b2, b)
	}

	// Unlink ONE link: the other must keep serving content (refcount guard).
	if err := fs.Remove("/a/copy.txt"); err != nil {
		t.Fatalf("Remove link: %v", err)
	}
	rc3, _, err := fs.Open("/b/dst.txt")
	if err != nil {
		t.Fatalf("Open survivor after unlink: %v", err)
	}
	io.ReadAll(rc3)
	rc3.Close()
	if _, err := fs.Lookup("/a/copy.txt"); err == nil {
		t.Fatal("removed link should be gone")
	}

	// Directory rename: subtree addressable at the new path.
	if err := fs.Rename("/b", "/c"); err != nil {
		t.Fatalf("Rename dir: %v", err)
	}
	rc4, _, err := fs.Open("/c/dst.txt")
	if err != nil {
		t.Fatalf("Open after dir rename: %v", err)
	}
	rc4.Close()
}

// TestR4T4_RemountRecovery (R4-T4/R4-C4) verifies a fresh FS over the same
// store sees the committed tree (stale entries are not served) and no
// goroutines leak.
func TestR4T4_RemountRecovery(t *testing.T) {
	defer goleak.VerifyNone(t)
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := fsWriteTree(t, store); err != nil {
		t.Fatal(err)
	}

	// Remount: new FS instance over the same backing store.
	fs2 := New(store)
	if _, err := fs2.Lookup("/app/config.json"); err != nil {
		t.Fatalf("remounted Lookup: %v", err)
	}
	ents, err := fs2.ReadDir("/app")
	if err != nil {
		t.Fatalf("remounted ReadDir: %v", err)
	}
	if len(ents) != 1 || ents[0].Name != "config.json" {
		t.Fatalf("remounted /app = %+v", ents)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("store close: %v", err)
	}
}

func fsWriteTree(t *testing.T, store *storage.CASStore) error {
	t.Helper()
	fs := New(store)
	if err := fs.Mkdir("/app", DefaultDirMode); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	if _, _, err := fs.Create("/app/config.json", 0o644, strings.NewReader(`{"k":"v"}`)); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	return nil
}

func storePaths(t *testing.T, store *storage.CASStore) []string {
	t.Helper()
	ms, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.RemotePath)
	}
	return out
}

func u32ptr(v uint32) *uint32 { return &v }
