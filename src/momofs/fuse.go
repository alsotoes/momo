// FUSE transport for momofs (R4, tasks.md #2 / #932).
//
// The momofs core is a content-addressed, write-whole-file store
// (dirs = JSON manifests, files = CAS blobs). The kernel speaks byte-range
// opens/writes; this adapter reconciles the two by buffering handle writes in
// memory and materializing the file as one content-addressed blob on Flush
// (release). Reads are served straight from the store via *FS.ReadAt.
//
// The adapter implements the bazil.org/fuse/fs node interfaces over the
// existing *FS core, so it is a pure client-side transport: the store backing
// it is the same CASStore the gateway and native client use.
package momofs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"

	"github.com/alsotoes/momo/src/storage"
)

// ServeFUSE mounts fs over mountpoint and serves the FUSE connection until
// the filesystem is unmounted, ctx is cancelled (which force-closes the
// connection), or a transport-level failure occurs. A clean unmount or
// cancellation returns nil.
func ServeFUSE(ctx context.Context, mountpoint string, store storage.Store, opts ...Option) error {
	c, err := fuse.Mount(mountpoint)
	if err != nil {
		return fmt.Errorf("momofs: fuse mount %s: %w", mountpoint, err)
	}

	root := newFuseRoot(New(store, opts...))

	type result struct {
		err error
	}
	serveDone := make(chan result, 1)
	go func() {
		// Serve blocks until unmount or the connection is closed (io.EOF after
		// a clean unmount). Any other return is a transport-level error.
		serveDone <- result{err: fs.Serve(c, root)}
	}()

	select {
	case r := <-serveDone:
		_ = c.Close()
		if r.err != nil && r.err != io.EOF {
			return fmt.Errorf("momofs: fuse serve: %w", r.err)
		}
		return nil
	case <-ctx.Done():
		// A userspace c.Close() alone does not reliably interrupt the
		// blocking /dev/fuse reader; detaching the mount (kernel-side) is what
		// unblocks it. Unmount first (best-effort; may already be gone), then
		// close, so the serve goroutine exits promptly.
		_ = fuse.Unmount(mountpoint)
		_ = c.Close()
		// Drain the serve goroutine so it never leaks its /dev/fuse reader.
		if r := <-serveDone; r.err != nil && r.err != io.EOF {
			return fmt.Errorf("momofs: fuse serve: %w", r.err)
		}
		return nil
	}
}

// UnmountFUSE forces a FUSE unmount. Used by the CLI on signal or by tests.
func UnmountFUSE(mountpoint string) error {
	return fuse.Unmount(mountpoint)
}

// fuseRoot is the root directory node. It owns the core FS and node cache,
// and embeds a fuseDir rooted at "/" so the root itself serves directory ops.
// It also satisfies fs.FS so it can be served directly (Root() returns itself).
type fuseRoot struct {
	fs *FS

	mu    sync.Mutex
	nodes map[string]fs.Node // path -> node (empty for none yet)

	fuseDir
}

var (
	_ fs.FS                 = (*fuseRoot)(nil)
	_ fs.Node               = (*fuseRoot)(nil)
	_ fs.NodeStringLookuper = (*fuseRoot)(nil)
	_ fs.NodeGetattrer      = (*fuseRoot)(nil)
	_ fs.NodeSetattrer      = (*fuseRoot)(nil)
	_ fs.NodeMkdirer        = (*fuseRoot)(nil)
	_ fs.NodeCreater        = (*fuseRoot)(nil)
	_ fs.NodeRemover        = (*fuseRoot)(nil)
	_ fs.NodeRenamer        = (*fuseRoot)(nil)
	_ fs.NodeLinker         = (*fuseRoot)(nil)
	_ fs.NodeOpener         = (*fuseRoot)(nil)
	_ fs.NodeForgetter      = (*fuseRoot)(nil)
)

// newFuseRoot wires the root node's embedded dir to itself at "/".
func newFuseRoot(f *FS) *fuseRoot {
	r := &fuseRoot{fs: f}
	r.fuseDir = fuseDir{root: r, path: "/"}
	return r
}

// Root returns the root node (itself), satisfying bazil.org/fuse/fs.FS.
func (r *fuseRoot) Root() (fs.Node, error) { return r, nil }

func (r *fuseRoot) attrFromEntry(p string, e *Entry, attr *fuse.Attr) {
	mode := e.Mode & 0o7777
	var fm os.FileMode
	if e.Type == EntryDir {
		fm = os.FileMode(mode) | os.ModeDir
	} else {
		fm = os.FileMode(mode)
	}
	attr.Mode = fm
	attr.Size = uint64(e.Size)
	attr.Uid = e.UID
	attr.Gid = e.GID
	if e.MTime > 0 {
		attr.Mtime = time.Unix(0, e.MTime)
		attr.Ctime = attr.Mtime
	}
	// Inode 0 tells bazil to generate a stable dynamic inode.
}

func entryIsDir(e *Entry) bool { return e != nil && e.Type == EntryDir }

// nodeFor returns a Node for the given absolute path, caching per-path nodes
// so repeated lookups reuse the same NodeID (bazil guidance; keeps kernel
// cache invalidations minimal).
func (r *fuseRoot) nodeFor(p string) (fs.Node, error) {
	e, err := r.fs.Lookup(p)
	if err != nil {
		return nil, toFuseErr(p, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.nodes == nil {
		r.nodes = make(map[string]fs.Node)
	}
	if n, ok := r.nodes[p]; ok {
		return n, nil
	}
	var n fs.Node
	if entryIsDir(e) {
		n = &fuseDir{root: r, path: p}
	} else {
		n = &fuseFile{root: r, path: p}
	}
	r.nodes[p] = n
	return n, nil
}

// toFuseErr maps a momofs error to a kernel errno, preserving the syscall
// errno the core already selected (EINVAL/ENOENT/EISDIR/...). Non-syscall
// errors become EIO rather than leaking into the kernel as opaque strings.
func toFuseErr(p string, err error) error {
	if err == nil {
		return nil
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno
	}
	log.Printf("momofs: fuse op %s: %v", p, err)
	return syscall.EIO
}

// ---- root / directory nodes ------------------------------------------------

// fuseDir implements the FUSE node interface for a directory entry.
type fuseDir struct {
	root *fuseRoot
	path string
}

var (
	_ fs.Node               = (*fuseDir)(nil)
	_ fs.NodeStringLookuper = (*fuseDir)(nil)
	_ fs.NodeGetattrer      = (*fuseDir)(nil)
	_ fs.NodeSetattrer      = (*fuseDir)(nil)
	_ fs.NodeMkdirer        = (*fuseDir)(nil)
	_ fs.NodeCreater        = (*fuseDir)(nil)
	_ fs.NodeRemover        = (*fuseDir)(nil)
	_ fs.NodeRenamer        = (*fuseDir)(nil)
	_ fs.NodeLinker         = (*fuseDir)(nil)
	_ fs.NodeOpener         = (*fuseDir)(nil)
	_ fs.NodeForgetter      = (*fuseDir)(nil)
)

func (d *fuseDir) Attr(ctx context.Context, a *fuse.Attr) error {
	e, err := d.root.fs.Lookup(d.path)
	if err != nil {
		return toFuseErr(d.path, err)
	}
	d.root.attrFromEntry(d.path, e, a)
	return nil
}

func (d *fuseDir) Getattr(ctx context.Context, req *fuse.GetattrRequest, resp *fuse.GetattrResponse) error {
	return d.Attr(ctx, &resp.Attr)
}

func (d *fuseDir) Setattr(ctx context.Context, req *fuse.SetattrRequest, resp *fuse.SetattrResponse) error {
	var mode, uid, gid *uint32
	var mtime *int64
	if req.Valid.Mode() {
		m := uint32(req.Mode.Perm())
		mode = &m
	}
	if req.Valid.Uid() {
		u := req.Uid
		uid = &u
	}
	if req.Valid.Gid() {
		g := req.Gid
		gid = &g
	}
	if req.Valid.Mtime() {
		mt := req.Mtime.UnixNano()
		mtime = &mt
	}
	if _, err := d.root.fs.SetAttr(d.path, mode, uid, gid, mtime); err != nil {
		return toFuseErr(d.path, err)
	}
	return d.Attr(ctx, &resp.Attr)
}

func (d *fuseDir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	p := joinPath(d.path, name)
	return d.root.nodeFor(p)
}

func (d *fuseDir) Mkdir(ctx context.Context, req *fuse.MkdirRequest) (fs.Node, error) {
	p := joinPath(d.path, req.Name)
	if err := d.root.fs.Mkdir(p, uint32(req.Mode.Perm())); err != nil {
		return nil, toFuseErr(p, err)
	}
	return d.root.nodeFor(p)
}

func (d *fuseDir) Create(ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (fs.Node, fs.Handle, error) {
	p := joinPath(d.path, req.Name)
	if _, _, err := d.root.fs.Create(p, uint32(req.Mode.Perm()), strings.NewReader("")); err != nil {
		return nil, nil, toFuseErr(p, err)
	}
	n, err := d.root.nodeFor(p)
	if err != nil {
		return nil, nil, toFuseErr(p, err)
	}
	h := &fuseFileHandle{root: d.root, path: p, mode: uint32(req.Mode.Perm())}
	return n, h, nil
}

func (d *fuseDir) Remove(ctx context.Context, req *fuse.RemoveRequest) error {
	p := joinPath(d.path, req.Name)
	if err := d.root.fs.Remove(p); err != nil {
		return toFuseErr(p, err)
	}
	return nil
}

func (d *fuseDir) Rename(ctx context.Context, req *fuse.RenameRequest, newDir fs.Node) error {
	nd, ok := newDir.(*fuseDir)
	if !ok {
		return syscall.EXDEV
	}
	src := joinPath(d.path, req.OldName)
	dst := joinPath(nd.path, req.NewName)
	if err := d.root.fs.Rename(src, dst); err != nil {
		return toFuseErr(src, err)
	}
	return nil
}

func (d *fuseDir) Link(ctx context.Context, req *fuse.LinkRequest, old fs.Node) (fs.Node, error) {
	oldNode, ok := old.(*fuseFile)
	if !ok {
		return nil, syscall.EINVAL
	}
	dst := joinPath(d.path, req.NewName)
	if err := d.root.fs.Link(oldNode.path, dst); err != nil {
		return nil, toFuseErr(dst, err)
	}
	return d.root.nodeFor(dst)
}

func (d *fuseDir) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	return &fuseDirHandle{root: d.root, path: d.path}, nil
}

func (d *fuseDir) Forget() {}

// ---- dir handle (readdir) ---------------------------------------------------

type fuseDirHandle struct {
	root *fuseRoot
	path string
}

var _ fs.HandleReadDirAller = (*fuseDirHandle)(nil)

func (h *fuseDirHandle) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	entries, err := h.root.fs.ReadDir(h.path)
	if err != nil {
		return nil, toFuseErr(h.path, err)
	}
	names := make([]fuse.Dirent, 0, len(entries)+2)
	names = append(names, fuse.Dirent{Type: fuse.DT_Dir, Name: "."})
	names = append(names, fuse.Dirent{Type: fuse.DT_Dir, Name: ".."})
	for _, e := range entries {
		dt := fuse.DT_File
		if e.Type == EntryDir {
			dt = fuse.DT_Dir
		}
		names = append(names, fuse.Dirent{Type: dt, Name: e.Name})
	}
	return names, nil
}

// ---- file node ----------------------------------------------------------------

// fuseFile implements the FUSE node interface for a regular file.
type fuseFile struct {
	root *fuseRoot
	path string
}

var (
	_ fs.Node          = (*fuseFile)(nil)
	_ fs.NodeGetattrer = (*fuseFile)(nil)
	_ fs.NodeSetattrer = (*fuseFile)(nil)
	_ fs.NodeOpener    = (*fuseFile)(nil)
	_ fs.NodeForgetter = (*fuseFile)(nil)
)

func (f *fuseFile) Attr(ctx context.Context, a *fuse.Attr) error {
	e, err := f.root.fs.Lookup(f.path)
	if err != nil {
		return toFuseErr(f.path, err)
	}
	f.root.attrFromEntry(f.path, e, a)
	return nil
}

func (f *fuseFile) Getattr(ctx context.Context, req *fuse.GetattrRequest, resp *fuse.GetattrResponse) error {
	return f.Attr(ctx, &resp.Attr)
}

func (f *fuseFile) Setattr(ctx context.Context, req *fuse.SetattrRequest, resp *fuse.SetattrResponse) error {
	var mode, uid, gid *uint32
	var mtime *int64
	if req.Valid.Mode() {
		m := uint32(req.Mode.Perm())
		mode = &m
	}
	if req.Valid.Uid() {
		u := req.Uid
		uid = &u
	}
	if req.Valid.Gid() {
		g := req.Gid
		gid = &g
	}
	if req.Valid.Mtime() {
		mt := req.Mtime.UnixNano()
		mtime = &mt
	}
	// Size truncation is not yet supported (byte-range correctness is a
	// follow-up task); report the current attrs so clients see consistent
	// metadata. mode/owner/mtime update the manifest entry.
	if _, err := f.root.fs.SetAttr(f.path, mode, uid, gid, mtime); err != nil {
		return toFuseErr(f.path, err)
	}
	return f.Attr(ctx, &resp.Attr)
}

func (f *fuseFile) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	// Readable opens serve straight from the store; writable opens materialize
	// on flush.
	e, err := f.root.fs.Lookup(f.path)
	if err != nil {
		return nil, toFuseErr(f.path, err)
	}
	return newFuseFileHandle(f.root, f.path, e), nil
}

func (f *fuseFile) Forget() {}

// ---- file handle ---------------------------------------------------------------

// fuseFileHandle buffers writes for one open file. On Flush/Release it
// materializes the whole file as a single content-addressed blob via *FS.Create
// (matching the store write-whole-file CAS model). Reads are served from the
// buffered view so read-modify-write of a dirty file stays consistent.
type fuseFileHandle struct {
	root *fuseRoot
	path string
	mode uint32

	mu     sync.Mutex
	data   []byte // full logical file content snapshot
	loaded bool   // data reflects the current store content
	dirty  bool   // data differs from the stored blob
}

var (
	_ fs.HandleReader    = (*fuseFileHandle)(nil)
	_ fs.HandleReadAller = (*fuseFileHandle)(nil)
	_ fs.HandleWriter    = (*fuseFileHandle)(nil)
	_ fs.HandleFlusher   = (*fuseFileHandle)(nil)
	_ fs.HandleReleaser  = (*fuseFileHandle)(nil)
)

func newFuseFileHandle(root *fuseRoot, p string, e *Entry) *fuseFileHandle {
	return &fuseFileHandle{root: root, path: p, mode: e.Mode}
}

// load reads the stored blob into data if not already loaded.
func (h *fuseFileHandle) load() error {
	if h.loaded {
		return nil
	}
	rc, e, err := h.root.fs.Open(h.path)
	if err != nil {
		return toFuseErr(h.path, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return syscall.EIO
	}
	if int64(len(data)) != e.Size {
		// Never truncate on a metadata skid; keep the blob as the source of
		// truth over a stale manifest size.
	}
	h.data = data
	h.loaded = true
	return nil
}

func (h *fuseFileHandle) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.load(); err != nil {
		return err
	}
	if req.Offset >= int64(len(h.data)) {
		resp.Data = resp.Data[:0]
		return nil
	}
	end := req.Offset + int64(len(resp.Data))
	if end > int64(len(h.data)) {
		end = int64(len(h.data))
	}
	resp.Data = append(resp.Data[:0], h.data[req.Offset:end]...)
	return nil
}

func (h *fuseFileHandle) ReadAll(ctx context.Context) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.load(); err != nil {
		return nil, err
	}
	return append([]byte(nil), h.data...), nil
}

func (h *fuseFileHandle) Write(ctx context.Context, req *fuse.WriteRequest, resp *fuse.WriteResponse) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.load(); err != nil {
		return err
	}
	need := req.Offset + int64(len(req.Data))
	if need > int64(cap(h.data)) {
		newData := make([]byte, need)
		copy(newData, h.data)
		h.data = newData
	} else if need > int64(len(h.data)) {
		h.data = h.data[:need]
	}
	copy(h.data[req.Offset:], req.Data)
	h.dirty = true
	resp.Size = len(req.Data)
	return nil
}

// flush materializes the buffered content into the store if dirty.
func (h *fuseFileHandle) flush() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.dirty {
		return nil
	}
	if _, _, err := h.root.fs.Create(h.path, h.mode, bytes.NewReader(h.data)); err != nil {
		return toFuseErr(h.path, err)
	}
	h.dirty = false
	return nil
}

func (h *fuseFileHandle) Flush(ctx context.Context, req *fuse.FlushRequest) error {
	return h.flush()
}

func (h *fuseFileHandle) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	return h.flush()
}

// ---- path helpers -----------------------------------------------------------

func joinPath(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return path.Join(dir, name)
}
