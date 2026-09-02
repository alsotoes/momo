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

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/alsotoes/momo/src/storage"
	"go.uber.org/goleak"
)

func errorsIsENOENT(err error) bool {
	return errors.Is(err, syscall.ENOENT)
}

// newTestRoot builds a fuseRoot over a fresh local CAS store backed by a
// temp dir, mirroring newTestFS from momofs_test.go. The root inode is wired
// to a go-fuse bridge via NewNodeFS (no mount required) so node-level tests
// can exercise Lookup/Create/Readdir without /dev/fuse.
func newTestRoot(t *testing.T, opts ...Option) (*fuseDir, *storage.CASStore) {
	t.Helper()
	s, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	root := newFuseRoot(New(s, opts...))
	fs.NewNodeFS(root, &fs.Options{})
	return root, s
}

func TestFuseUnit_LookupCreateReadWriteFlush(t *testing.T) {
	defer goleak.VerifyNone(t)
	root, _ := newTestRoot(t)

	ctx := context.Background()

	// Root resolves as a directory.
	var out fuse.EntryOut
	if _, errno := root.Lookup(ctx, "docs", &out); errno != syscall.ENOENT {
		t.Fatalf("missing entry: want ENOENT, got %v", errno)
	}

	// mkdir via node interface.
	var mout fuse.EntryOut
	d, errno := root.Mkdir(ctx, "docs", 0o755, &mout)
	if errno != 0 {
		t.Fatalf("Mkdir: %v", errno)
	}
	ddir := d.Operations().(*fuseDir)
	if ddir.path != "/docs" {
		t.Fatalf("dir path = %q, want /docs", ddir.path)
	}

	// create a file, return node+handle.
	var cout fuse.EntryOut
	fnode, handle, _, errno := ddir.Create(ctx, "note.txt", 0, 0o640, &cout)
	if errno != 0 {
		t.Fatalf("Create: %v", errno)
	}
	_ = fnode
	h := handle.(*fuseFileHandle)
	if h.path != "/docs/note.txt" {
		t.Fatalf("file handle path = %q, want /docs/note.txt", h.path)
	}

	// write a couple of chunks, then read them back from the handle buffer.
	if _, errno := h.Write(ctx, []byte("hello "), 0); errno != 0 {
		t.Fatalf("Write: %v", errno)
	}
	if _, errno := h.Write(ctx, []byte("momofs!"), 6); errno != 0 {
		t.Fatalf("Write2: %v", errno)
	}

	// Flush materializes the whole file as one CAS blob.
	if errno := h.Flush(ctx); errno != 0 {
		t.Fatalf("Flush: %v", errno)
	}

	// Store now holds exactly one object for the file.
	buf := make([]byte, 64)
	n, err := root.root.core.ReadAt("/docs/note.txt", 0, buf)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAt: %v", err)
	}
	got := buf[:n]
	if string(got) != "hello momofs!" {
		t.Fatalf("ReadAt = %q, want %q", got, "hello momofs!")
	}

	// Attr reflects file size + mode.
	fa, err := root.root.core.GetAttr("/docs/note.txt")
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
	stream, errno := ddir.Readdir(ctx)
	if errno != 0 {
		t.Fatalf("Readdir: %v", errno)
	}
	var names []string
	for stream.HasNext() {
		de, e2 := stream.Next()
		if e2 != 0 {
			t.Fatalf("Readdir Next: %v", e2)
		}
		names = append(names, de.Name)
	}
	stream.Close()
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
	var mout fuse.EntryOut
	d, errno := root.Mkdir(ctx, "d", 0o755, &mout)
	if errno != 0 {
		t.Fatalf("Mkdir: %v", errno)
	}
	ddir := d.Operations().(*fuseDir)
	var cout fuse.EntryOut
	_, handle, _, errno := ddir.Create(ctx, "a.txt", 0, 0o644, &cout)
	if errno != 0 {
		t.Fatalf("Create: %v", errno)
	}
	if _, errno := handle.(*fuseFileHandle).Write(ctx, []byte("aaa"), 0); errno != 0 {
		t.Fatalf("Write: %v", errno)
	}
	if errno := handle.(*fuseFileHandle).Flush(ctx); errno != 0 {
		t.Fatalf("Flush: %v", errno)
	}

	// rename /d/a.txt -> /d/b.txt
	if errno := ddir.Rename(ctx, "a.txt", ddir, "b.txt", 0); errno != 0 {
		t.Fatalf("Rename: %v", errno)
	}
	if _, err := root.root.core.Lookup("/d/a.txt"); !errorsIsENOENT(err) {
		t.Fatalf("old name should be gone, got %v", err)
	}
	if _, err := root.root.core.Lookup("/d/b.txt"); err != nil {
		t.Fatalf("new name missing: %v", err)
	}

	// hardlink /d/b.txt -> /d/c.txt
	var lout fuse.EntryOut
	fnode, errno := root.root.nodeFor(ctx, &ddir.Inode, "/d/b.txt", fuse.S_IFREG)
	if errno != 0 {
		t.Fatalf("nodeFor b.txt: %v", errno)
	}
	if _, errno := ddir.Link(ctx, fnode, "c.txt", &lout); errno != 0 {
		t.Fatalf("Link: %v", errno)
	}
	// both link names resolve to the same content
	bbuf, ccbuf := make([]byte, 16), make([]byte, 16)
	bn, _ := root.root.core.ReadAt("/d/b.txt", 0, bbuf)
	cn, _ := root.root.core.ReadAt("/d/c.txt", 0, ccbuf)
	if string(bbuf[:bn]) != string(ccbuf[:cn]) {
		t.Fatalf("link contents differ: %q vs %q", bbuf[:bn], ccbuf[:cn])
	}

	// unlink
	if errno := ddir.Unlink(ctx, "b.txt"); errno != 0 {
		t.Fatalf("Unlink: %v", errno)
	}
	if _, err := root.root.core.Lookup("/d/b.txt"); !errorsIsENOENT(err) {
		t.Fatalf("unlinked name should be gone, got %v", err)
	}
}

func TestFuseUnit_NodeCacheStable(t *testing.T) {
	defer goleak.VerifyNone(t)
	root, store := newTestRoot(t)
	_ = store

	if err := root.root.core.Mkdir("/stable", DefaultDirMode); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if _, _, err := root.root.core.Create("/stable/x", 0o644, strings.NewReader("x")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	n1, errno := root.root.nodeFor(context.Background(), &root.Inode, "/stable/x", fuse.S_IFREG)
	if errno != 0 {
		t.Fatalf("nodeFor 1: %v", errno)
	}
	n2, errno := root.root.nodeFor(context.Background(), &root.Inode, "/stable/x", fuse.S_IFREG)
	if errno != 0 {
		t.Fatalf("nodeFor 2: %v", errno)
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

	if err := root.root.core.Mkdir("/m", 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if _, _, err := root.root.core.Create("/m/f", 0o600, strings.NewReader("12345")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var mout fuse.EntryOut
	dn, errno := root.Lookup(ctx, "m", &mout)
	if errno != 0 {
		t.Fatalf("Lookup dir: %v", errno)
	}
	ddir := dn.Operations().(*fuseDir)
	var aout fuse.AttrOut
	if errno := ddir.Getattr(ctx, nil, &aout); errno != 0 {
		t.Fatalf("dir Getattr: %v", errno)
	}
	if aout.Mode&fuse.S_IFDIR == 0 || aout.Mode&0o777 != 0o750 {
		t.Fatalf("dir mode = %o, want dir+750", aout.Mode)
	}

	var fout fuse.EntryOut
	fn, errno := ddir.Lookup(ctx, "f", &fout)
	if errno != 0 {
		t.Fatalf("Lookup file: %v", errno)
	}
	ffile := fn.Operations().(*fuseFile)
	var fa fuse.AttrOut
	if errno := ffile.Getattr(ctx, nil, &fa); errno != 0 {
		t.Fatalf("file Getattr: %v", errno)
	}
	if fa.Mode&fuse.S_IFDIR != 0 || fa.Mode&0o777 != 0o600 {
		t.Fatalf("file mode = %o, want 600", fa.Mode)
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
