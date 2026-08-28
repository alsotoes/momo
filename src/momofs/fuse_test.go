package momofs

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"

	"github.com/alsotoes/momo/src/storage"
	"go.uber.org/goleak"
)

func errorsIsENOENT(err error) bool {
	return errors.Is(err, syscall.ENOENT)
}

// newTestRoot builds a fuseRoot over a fresh local CAS store backed by a
// temp dir, mirroring newTestFS from momofs_test.go.
func newTestRoot(t *testing.T, opts ...Option) (*fuseRoot, *storage.CASStore) {
	t.Helper()
	s, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return newFuseRoot(New(s, opts...)), s
}

func TestFuseUnit_LookupCreateReadWriteFlush(t *testing.T) {
	defer goleak.VerifyNone(t)
	root, _ := newTestRoot(t)

	ctx := context.Background()

	// Root resolves as a directory.
	_, err := root.Lookup(ctx, "docs")
	if got := err; !errorsIsENOENT(got) {
		t.Fatalf("missing entry: want ENOENT, got %v", err)
	}

	// mkdir via node interface.
	d, err := root.Mkdir(ctx, &fuse.MkdirRequest{Name: "docs", Mode: 0o755})
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	ddir := d.(*fuseDir)
	if ddir.path != "/docs" {
		t.Fatalf("dir path = %q, want /docs", ddir.path)
	}

	// create a file, return node+handle.
	fileNode, handle, err := ddir.Create(ctx, &fuse.CreateRequest{Name: "note.txt", Mode: 0o640}, &fuse.CreateResponse{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = fileNode
	h := handle.(*fuseFileHandle)
	if h.path != "/docs/note.txt" {
		t.Fatalf("file handle path = %q, want /docs/note.txt", h.path)
	}

	// write a couple of chunks, then read them back from the handle buffer.
	w := &fuse.WriteRequest{Offset: 0, Data: []byte("hello ")}
	rsp := &fuse.WriteResponse{}
	if err := h.Write(ctx, w, rsp); err != nil {
		t.Fatalf("Write: %v", err)
	}
	w2 := &fuse.WriteRequest{Offset: 6, Data: []byte("momofs!")}
	if err := h.Write(ctx, w2, rsp); err != nil {
		t.Fatalf("Write2: %v", err)
	}

	// Flush materializes the whole file as one CAS blob.
	if err := h.Flush(ctx, nil); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Store now holds exactly one object for the file.
	buf := make([]byte, 64)
	n, err := root.fs.ReadAt("/docs/note.txt", 0, buf)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAt: %v", err)
	}
	got := buf[:n]
	if string(got) != "hello momofs!" {
		t.Fatalf("ReadAt = %q, want %q", got, "hello momofs!")
	}

	// Attr reflects file size + mode.
	fa, err := root.fs.GetAttr("/docs/note.txt")
	if err != nil {
		t.Fatalf("GetAttr: %v", err)
	}
	if fa.Size != int64(len("hello momofs!")) {
		t.Fatalf("size = %d, want %d", fa.Size, len("hello momofs!"))
	}
	if fa.Mode&0o777 != 0o640 {
		t.Fatalf("mode = %o, want 640", fa.Mode)
	}

	// ReadDir lists both dot entries plus the file.
	dh := &fuseDirHandle{root: root, path: "/docs"}
	ents, err := dh.ReadDirAll(ctx)
	if err != nil {
		t.Fatalf("ReadDirAll: %v", err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{".", "..", "note.txt"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("readdir missing %q: %v", want, names)
		}
	}
}

func TestFuseUnit_RemoveRenameLink(t *testing.T) {
	defer goleak.VerifyNone(t)
	root, _ := newTestRoot(t)
	ctx := context.Background()

	// mkdir /d, create /d/a.txt
	d, err := root.Mkdir(ctx, &fuse.MkdirRequest{Name: "d", Mode: 0o755})
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	ddir := d.(*fuseDir)
	_, h, err := ddir.Create(ctx, &fuse.CreateRequest{Name: "a.txt", Mode: 0o644}, &fuse.CreateResponse{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.(*fuseFileHandle).Write(ctx, &fuse.WriteRequest{Offset: 0, Data: []byte("aaa")}, &fuse.WriteResponse{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := h.(*fuseFileHandle).Flush(ctx, nil); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// rename /d/a.txt -> /d/b.txt
	if err := ddir.Rename(ctx, &fuse.RenameRequest{OldName: "a.txt", NewName: "b.txt"}, ddir); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := root.fs.Lookup("/d/a.txt"); !errorsIsENOENT(err) {
		t.Fatalf("old name should be gone, got %v", err)
	}
	if _, err := root.fs.Lookup("/d/b.txt"); err != nil {
		t.Fatalf("new name missing: %v", err)
	}

	// hardlink /d/b.txt -> /d/c.txt
	fnode, err := root.nodeFor("/d/b.txt")
	if err != nil {
		t.Fatalf("nodeFor b.txt: %v", err)
	}
	if _, err := ddir.Link(ctx, &fuse.LinkRequest{NewName: "c.txt"}, fnode); err != nil {
		t.Fatalf("Link: %v", err)
	}
	// both link names resolve to the same content
	bbuf, ccbuf := make([]byte, 16), make([]byte, 16)
	bn, _ := root.fs.ReadAt("/d/b.txt", 0, bbuf)
	cn, _ := root.fs.ReadAt("/d/c.txt", 0, ccbuf)
	if string(bbuf[:bn]) != string(ccbuf[:cn]) {
		t.Fatalf("link contents differ: %q vs %q", bbuf[:bn], ccbuf[:cn])
	}

	// unlink
	if err := ddir.Remove(ctx, &fuse.RemoveRequest{Name: "b.txt"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := root.fs.Lookup("/d/b.txt"); !errorsIsENOENT(err) {
		t.Fatalf("unlinked name should be gone, got %v", err)
	}
}

func TestFuseUnit_NodeCacheStable(t *testing.T) {
	defer goleak.VerifyNone(t)
	root, store := newTestRoot(t)
	_ = store

	if err := root.fs.Mkdir("/stable", DefaultDirMode); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if _, _, err := root.fs.Create("/stable/x", 0o644, strings.NewReader("x")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	n1, err := root.nodeFor("/stable/x")
	if err != nil {
		t.Fatalf("nodeFor 1: %v", err)
	}
	n2, err := root.nodeFor("/stable/x")
	if err != nil {
		t.Fatalf("nodeFor 2: %v", err)
	}
	if n1 != n2 {
		t.Fatalf("same path returned different nodes (cache not stable): %v vs %v", n1, n2)
	}
}

// TestFuseUnit_AttrMapping verifies file/dir modes, sizes and times map onto
// the FUSE Attr without kernel involvement.
func TestFuseUnit_AttrMapping(t *testing.T) {
	defer goleak.VerifyNone(t)
	root, _ := newTestRoot(t)
	ctx := context.Background()

	if err := root.fs.Mkdir("/m", 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if _, _, err := root.fs.Create("/m/f", 0o600, strings.NewReader("12345")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	d, err := root.nodeFor("/m")
	if err != nil {
		t.Fatalf("nodeFor dir: %v", err)
	}
	var a fuse.Attr
	if err := d.(fs.Node).Attr(ctx, &a); err != nil {
		t.Fatalf("dir Attr: %v", err)
	}
	if a.Mode&os.ModeDir == 0 || a.Mode.Perm() != 0o750 {
		t.Fatalf("dir mode = %v, want dir+750", a.Mode)
	}

	f, err := root.nodeFor("/m/f")
	if err != nil {
		t.Fatalf("nodeFor file: %v", err)
	}
	var fa fuse.Attr
	if err := f.(fs.Node).Attr(ctx, &fa); err != nil {
		t.Fatalf("file Attr: %v", err)
	}
	if fa.Mode&os.ModeDir != 0 || fa.Mode.Perm() != 0o600 {
		t.Fatalf("file mode = %v, want 600", fa.Mode)
	}
	if fa.Size != 5 {
		t.Fatalf("file size = %d, want 5", fa.Size)
	}
}

// TestFuseE2E_MountRoundTrip mounts the FS at a real FUSE mountpoint and
// drives it through the kernel, skipping when /dev/fuse or fusermount is
// unavailable (CI-friendly; local hosts can run it).
func TestFuseE2E_MountRoundTrip(t *testing.T) {
	if _, err := os.Stat("/dev/fuse"); err != nil {
		t.Skip(" /dev/fuse unavailable")
	}
	if _, err := exec.LookPath("fusermount"); err != nil {
		if _, err2 := exec.LookPath("fusermount3"); err2 != nil {
			t.Skip(" fusermount unavailable")
		}
	}

	mountPoint := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		t.Fatalf("mkdir mountpoint: %v", err)
	}

	// Background serve; e2e shares the store with direct writes to prove
	// native <-> mount visibility (R4-C3).
	store, err := storage.NewCASStore(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}

	serveCtx, serveCancel := context.WithCancel(context.Background())
	var serveErr error
	serveFinished := make(chan struct{})
	go func() {
		serveErr = ServeFUSE(serveCtx, mountPoint, store)
		close(serveFinished)
	}()

	// Wait until the FUSE root actually serves reads. A bare stat or readdir
	// succeeds even before the filesystem is mounted (the mountpoint dir
	// exists), so we prove real negotiation by writing a marker natively and
	// polling for it through the mount.
	native := New(store)
	if _, _, err := native.Create("/.probe", 0o644, strings.NewReader("probe")); err != nil {
		t.Fatalf("marker create: %v", err)
	}
	var ready bool
	for i := 0; i < 200; i++ {
		select {
		case <-serveFinished:
			t.Fatalf("serve exited during readiness: %v", serveErr)
		default:
		}
		if b, err := os.ReadFile(filepath.Join(mountPoint, ".probe")); err == nil && string(b) == "probe" {
			ready = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !ready {
		t.Fatal("mount never became readable")
	}

	// Cleanup order matters: unmount so the serve goroutine exits, then close
	// the store, then goleak-verify. goleak is registered first so it runs
	// last (LIFO).
	defer goleak.VerifyNone(t)
	defer func() {
		serveCancel()
		select {
		case <-serveFinished:
		case <-time.After(10 * time.Second):
		}
		_ = UnmountFUSE(mountPoint)
		_ = store.Close()
	}()

	// Native write then mount read (and vice versa).
	if err := native.Mkdir("/app", DefaultDirMode); err != nil {
		t.Fatalf("native Mkdir: %v", err)
	}
	if _, _, err := native.Create("/app/config.json", 0o644, strings.NewReader(`{"k":"v"}`)); err != nil {
		t.Fatalf("native Create: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(mountPoint, "app", "config.json"))
	if err != nil {
		t.Fatalf("mount read of native file: %v", err)
	}
	if string(got) != `{"k":"v"}` {
		t.Fatalf("mount read = %q, want %q", got, `{"k":"v"}`)
	}

	// Mount write surfaced natively.
	if err := os.WriteFile(filepath.Join(mountPoint, "app", "new.txt"), []byte("from-mount"), 0o644); err != nil {
		t.Fatalf("mount write: %v", err)
	}
	got2 := make([]byte, 32)
	n2, err := native.ReadAt("/app/new.txt", 0, got2)
	if err != nil && err != io.EOF {
		t.Fatalf("native read of mount file: %v", err)
	}
	if string(got2[:n2]) != "from-mount" {
		t.Fatalf("native read = %q, want %q", got2[:n2], "from-mount")
	}

// Shut down: cancel ctx (ServeFUSE unmounts + closes conn so the serve
	// goroutine exits), then wait.
	serveCancel()
	select {
	case <-serveFinished:
		if serveErr != nil {
			t.Fatalf("serve returned error after cancel: %v", serveErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not return after cancel")
	}
}