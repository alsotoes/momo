// FUSE transport for momofs (R4, tasks.md #2 / #932).
//
// The momofs core is a content-addressed, write-whole-file store
// (dirs = JSON manifests, files = CAS blobs). The kernel speaks byte-range
// opens/writes; this adapter reconciles the two by buffering handle writes in
// memory and materializing the file as one content-addressed blob on Flush
// (release). Reads are served straight from the store via *FS.ReadAt.
//
// The adapter implements the go-fuse/v2 fs node interfaces over the existing
// *FS core, so it is a pure client-side transport: the store backing it is
// the same CASStore the gateway and native client use.
//
// Migration note (openspec/changes/add-fuse-implementation): this file
// replaced the prior bazil.org/fuse wire protocol with the go-fuse/v2
// high-level fs API (Issue #980). Error mapping follows the go-fuse
// convention of returning syscall errno values directly, preserving the
// syscall.Errno the core already selected (EINVAL/ENOENT/EISDIR/...).
package momofs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"path"
	"strings"
	"sync"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/alsotoes/momo/src/storage"
)

// activeServers tracks live FUSE servers by mountpoint so the CLI and tests
// can force an unmount by path (go-fuse/v2 exposes Unmount only on the
// *fuse.Server it returns from fs.Mount).
var activeServers sync.Map // mountpoint string -> *fuse.Server

// ServeFUSE mounts fs over mountpoint and serves the FUSE connection until
// the filesystem is unmounted, ctx is cancelled (which force-closes the
// connection), or a transport-level failure occurs. A clean unmount or
// cancellation returns nil.
func ServeFUSE(ctx context.Context, mountpoint string, store storage.Store, opts ...Option) error {
	root := newFuseRoot(New(store, opts...))

	server, err := fs.Mount(mountpoint, root, &fs.Options{
		MountOptions: fuse.MountOptions{},
	})
	if err != nil {
		return fmt.Errorf("momofs: fuse mount %s: %w", mountpoint, err)
	}
	activeServers.Store(mountpoint, server)
	defer activeServers.Delete(mountpoint)

	// fs.Mount starts the serve loop internally; Wait blocks until the
	// filesystem is unmounted (kernel-side or via server.Unmount).
	serveDone := make(chan error, 1)
	go func() {
		server.Wait()
		serveDone <- nil
	}()

	select {
	case <-serveDone:
		return nil
	case <-ctx.Done():
		// Cancellation: unmount so the kernel-side /dev/fuse reader
		// unblocks and the Wait goroutine exits promptly.
		_ = server.Unmount()
		<-serveDone
		return nil
	}
}

// UnmountFUSE forces a FUSE unmount. Used by the CLI on signal or by tests.
func UnmountFUSE(mountpoint string) error {
	if s, ok := activeServers.Load(mountpoint); ok {
		return s.(*fuse.Server).Unmount()
	}
	return nil
}

// fuseRoot is the shared state for one mounted filesystem: the core FS and
// the node cache. The mount's root node is a fuseDir rooted at "/".
type fuseRoot struct {
	core *FS

	mu    sync.Mutex
	nodes map[string]*fs.Inode // path -> inode (stable NodeID per path)
}

// newFuseRoot wires the root state and returns the root directory node.
func newFuseRoot(f *FS) *fuseDir {
	r := &fuseRoot{core: f}
	return &fuseDir{root: r, path: "/"}
}

func (r *fuseRoot) attrFromEntry(e *Entry, attr *fuse.Attr) {
	mode := e.Mode & 0o7777
	if e.Type == EntryDir {
		mode |= fuse.S_IFDIR
	} else {
		mode |= fuse.S_IFREG
	}
	attr.Mode = mode
	attr.Size = uint64(e.Size)
	attr.Uid = e.UID
	attr.Gid = e.GID
	if e.MTime > 0 {
		attr.Mtime = uint64(e.MTime / 1e9)
		attr.Mtimensec = uint32(e.MTime % 1e9)
		attr.Ctime = attr.Mtime
		attr.Ctimensec = attr.Mtimensec
	}
	attr.Blksize = 4096
	attr.Nlink = 1
	if e.Type == EntryDir {
		attr.Nlink = 2
	}
}

func entryIsDir(e *Entry) bool { return e != nil && e.Type == EntryDir }

// nodeFor returns the cached go-fuse inode for the given absolute path,
// creating it as a child of parent on first use. Caching per-path inodes
// keeps the kernel NodeID stable across repeated lookups (go-fuse guidance;
// minimises cache churn).
func (r *fuseRoot) nodeFor(ctx context.Context, parent *fs.Inode, p string, mode uint32) (*fs.Inode, syscall.Errno) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.nodes == nil {
		r.nodes = make(map[string]*fs.Inode)
	}
	if n, ok := r.nodes[p]; ok {
		return n, 0
	}
	var n *fs.Inode
	if mode&syscall.S_IFDIR != 0 {
		n = parent.NewInode(ctx, &fuseDir{root: r, path: p}, fs.StableAttr{Mode: mode})
	} else {
		n = parent.NewInode(ctx, &fuseFile{root: r, path: p}, fs.StableAttr{Mode: mode})
	}
	r.nodes[p] = n
	return n, 0
}

// fuseRecover is the Rule 43/37 panic guard for FUSE node callbacks. A panic
// in a callback must never cross the kernel bridge (it would tear down the
// whole mount); the two-line pattern logs and maps the failure to EIO so the
// offending op fails cleanly while the connection survives.
func fuseRecover(errno *syscall.Errno) {
	if r := recover(); r != nil {
		log.Printf("CRITICAL: Recovered from panic in momofs FUSE op: %v", r)
		*errno = syscall.EIO
	}
}

// toFuseErr maps a momofs error to a kernel errno, preserving the syscall
// errno the core already selected (EINVAL/ENOENT/EISDIR/...). Non-syscall
// errors become EIO rather than leaking into the kernel as opaque strings.
func toFuseErr(p string, err error) syscall.Errno {
	if err == nil {
		return 0
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno
	}
	log.Printf("momofs: fuse op %s: %v", p, err)
	return syscall.EIO
}

// ---- root / directory nodes ------------------------------------------------

// fuseDir implements the go-fuse fs directory node for a directory entry.
type fuseDir struct {
	fs.Inode
	root *fuseRoot
	path string
}

var (
	_ fs.NodeStatfser    = (*fuseDir)(nil)
	_ fs.NodeOnForgetter = (*fuseDir)(nil)
	_ fs.NodeLookuper    = (*fuseDir)(nil)
	_ fs.NodeGetattrer   = (*fuseDir)(nil)
	_ fs.NodeSetattrer   = (*fuseDir)(nil)
	_ fs.NodeMkdirer     = (*fuseDir)(nil)
	_ fs.NodeCreater     = (*fuseDir)(nil)
	_ fs.NodeUnlinker    = (*fuseDir)(nil)
	_ fs.NodeRmdirer     = (*fuseDir)(nil)
	_ fs.NodeRenamer     = (*fuseDir)(nil)
	_ fs.NodeLinker      = (*fuseDir)(nil)
	_ fs.NodeOpendirer   = (*fuseDir)(nil)
	_ fs.NodeReaddirer   = (*fuseDir)(nil)
)

// OnForget is a no-op placeholder for the inode lifecycle hook.
func (d *fuseDir) OnForget() {}

// Statfs reports filesystem-wide statistics.
func (d *fuseDir) Statfs(ctx context.Context, out *fuse.StatfsOut) (errno syscall.Errno) {
	defer fuseRecover(&errno)
	out.Bsize = 4096
	out.Frsize = 4096
	out.NameLen = 255
	return 0
}

// Getattr fills the FUSE attribute structure for the directory.
func (d *fuseDir) Getattr(ctx context.Context, _ fs.FileHandle, out *fuse.AttrOut) (errno syscall.Errno) {
	defer fuseRecover(&errno)
	e, err := d.root.core.Lookup(d.path)
	if err != nil {
		return toFuseErr(d.path, err)
	}
	d.root.attrFromEntry(e, &out.Attr)
	return 0
}

// Setattr applies attribute changes (mode, uid, gid, mtime) to the directory.
func (d *fuseDir) Setattr(ctx context.Context, _ fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) (errno syscall.Errno) {
	defer fuseRecover(&errno)
	var mode, uid, gid *uint32
	var mtime *int64
	if m, ok := in.GetMode(); ok {
		mode = &m
	}
	if u, ok := in.GetUID(); ok {
		uid = &u
	}
	if g, ok := in.GetGID(); ok {
		gid = &g
	}
	if mt, ok := in.GetMTime(); ok {
		v := mt.UnixNano()
		mtime = &v
	}
	if _, err := d.root.core.SetAttr(d.path, mode, uid, gid, mtime); err != nil {
		return toFuseErr(d.path, err)
	}
	return d.Getattr(ctx, nil, out)
}

// Lookup resolves a child name within the directory.
func (d *fuseDir) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (ch *fs.Inode, errno syscall.Errno) {
	defer fuseRecover(&errno)
	p := joinPath(d.path, name)
	e, err := d.root.core.Lookup(p)
	if err != nil {
		return nil, toFuseErr(p, err)
	}
	var mode uint32
	if entryIsDir(e) {
		mode = fuse.S_IFDIR
	} else {
		mode = fuse.S_IFREG
	}
	ch, errno = d.root.nodeFor(ctx, &d.Inode, p, mode)
	if errno != 0 {
		return nil, errno
	}
	d.root.attrFromEntry(e, &out.Attr)
	return ch, 0
}

// Mkdir creates a subdirectory entry.
func (d *fuseDir) Mkdir(ctx context.Context, name string, mode uint32, out *fuse.EntryOut) (ch *fs.Inode, errno syscall.Errno) {
	defer fuseRecover(&errno)
	p := joinPath(d.path, name)
	if err := d.root.core.Mkdir(p, mode&0o7777); err != nil {
		return nil, toFuseErr(p, err)
	}
	ch, errno = d.root.nodeFor(ctx, &d.Inode, p, fuse.S_IFDIR|(mode&0o7777))
	if errno != 0 {
		return nil, errno
	}
	e, err := d.root.core.Lookup(p)
	if err != nil {
		return nil, toFuseErr(p, err)
	}
	d.root.attrFromEntry(e, &out.Attr)
	return ch, 0
}

// Create creates a file in the directory and returns an inode plus a handle
// buffering subsequent writes until Flush materializes the CAS blob.
func (d *fuseDir) Create(ctx context.Context, name string, flags, mode uint32, out *fuse.EntryOut) (ch *fs.Inode, fh fs.FileHandle, _ uint32, errno syscall.Errno) {
	defer fuseRecover(&errno)
	p := joinPath(d.path, name)
	if _, _, err := d.root.core.Create(p, mode&0o7777, strings.NewReader("")); err != nil {
		return nil, nil, 0, toFuseErr(p, err)
	}
	ch, errno = d.root.nodeFor(ctx, &d.Inode, p, fuse.S_IFREG|(mode&0o7777))
	if errno != 0 {
		return nil, nil, 0, errno
	}
	e, err := d.root.core.Lookup(p)
	if err != nil {
		return nil, nil, 0, toFuseErr(p, err)
	}
	d.root.attrFromEntry(e, &out.Attr)
	return ch, &fuseFileHandle{root: d.root, path: p, mode: mode & 0o7777}, 0, 0
}

// Unlink removes the named child entry.
func (d *fuseDir) Unlink(ctx context.Context, name string) (errno syscall.Errno) {
	defer fuseRecover(&errno)
	p := joinPath(d.path, name)
	if err := d.root.core.Remove(p); err != nil {
		return toFuseErr(p, err)
	}
	return 0
}

// Rmdir removes the named child directory.
func (d *fuseDir) Rmdir(ctx context.Context, name string) (errno syscall.Errno) {
	defer fuseRecover(&errno)
	return d.Unlink(ctx, name)
}

// Rename moves the named entry to a new name in newParent.
func (d *fuseDir) Rename(ctx context.Context, name string, newParent fs.InodeEmbedder, newName string, flags uint32) (errno syscall.Errno) {
	defer fuseRecover(&errno)
	nd, ok := newParent.(*fuseDir)
	if !ok {
		return syscall.EXDEV
	}
	src := joinPath(d.path, name)
	dst := joinPath(nd.path, newName)
	if err := d.root.core.Rename(src, dst); err != nil {
		return toFuseErr(src, err)
	}
	return 0
}

// Link creates a hard link to the old node under the given name.
func (d *fuseDir) Link(ctx context.Context, target fs.InodeEmbedder, name string, out *fuse.EntryOut) (ch *fs.Inode, errno syscall.Errno) {
	defer fuseRecover(&errno)
	// target is an existing inode (the *fs.Inode), whose operations are the
	// momofs node it wraps.
	var tf *fuseFile
	switch ops := target.EmbeddedInode().Operations().(type) {
	case *fuseFile:
		tf = ops
	default:
		return nil, syscall.EINVAL
	}
	dst := joinPath(d.path, name)
	if err := d.root.core.Link(tf.path, dst); err != nil {
		return nil, toFuseErr(dst, err)
	}
	ch, errno = d.root.nodeFor(ctx, &d.Inode, dst, fuse.S_IFREG)
	if errno != 0 {
		return nil, errno
	}
	e, err := d.root.core.Lookup(dst)
	if err != nil {
		return nil, toFuseErr(dst, err)
	}
	d.root.attrFromEntry(e, &out.Attr)
	return ch, 0
}

// Opendir opens the directory for reading.
func (d *fuseDir) Opendir(ctx context.Context) (errno syscall.Errno) {
	defer fuseRecover(&errno)
	return 0
}

// Readdir returns all directory entries.
func (d *fuseDir) Readdir(ctx context.Context) (stream fs.DirStream, errno syscall.Errno) {
	defer fuseRecover(&errno)
	entries, err := d.root.core.ReadDir(d.path)
	if err != nil {
		return nil, toFuseErr(d.path, err)
	}
	names := make([]fuse.DirEntry, 0, len(entries)+2)
	names = append(names, fuse.DirEntry{Mode: fuse.S_IFDIR, Name: "."})
	names = append(names, fuse.DirEntry{Mode: fuse.S_IFDIR, Name: ".."})
	for _, e := range entries {
		mode := uint32(fuse.S_IFREG)
		if e.Type == EntryDir {
			mode = fuse.S_IFDIR
		}
		names = append(names, fuse.DirEntry{Mode: mode, Name: e.Name})
	}
	return fs.NewListDirStream(names), 0
}

// ---- file node ----------------------------------------------------------------

// fuseFile implements the go-fuse fs node for a regular file.
type fuseFile struct {
	fs.Inode
	root *fuseRoot
	path string
}

var (
	_ fs.NodeGetattrer = (*fuseFile)(nil)
	_ fs.NodeSetattrer = (*fuseFile)(nil)
	_ fs.NodeOpener    = (*fuseFile)(nil)
)

// Getattr fills the FUSE attribute structure for the file.
func (f *fuseFile) Getattr(ctx context.Context, _ fs.FileHandle, out *fuse.AttrOut) (errno syscall.Errno) {
	defer fuseRecover(&errno)
	e, err := f.root.core.Lookup(f.path)
	if err != nil {
		return toFuseErr(f.path, err)
	}
	f.root.attrFromEntry(e, &out.Attr)
	return 0
}

// Setattr applies attribute changes (mode, uid, gid, mtime) to the file.
func (f *fuseFile) Setattr(ctx context.Context, _ fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) (errno syscall.Errno) {
	defer fuseRecover(&errno)
	var mode, uid, gid *uint32
	var mtime *int64
	if m, ok := in.GetMode(); ok {
		mode = &m
	}
	if u, ok := in.GetUID(); ok {
		uid = &u
	}
	if g, ok := in.GetGID(); ok {
		gid = &g
	}
	if mt, ok := in.GetMTime(); ok {
		v := mt.UnixNano()
		mtime = &v
	}
	// Size truncation is not yet supported (byte-range correctness is a
	// follow-up task); report the current attrs so clients see consistent
	// metadata. mode/owner/mtime update the manifest entry.
	if _, err := f.root.core.SetAttr(f.path, mode, uid, gid, mtime); err != nil {
		return toFuseErr(f.path, err)
	}
	return f.Getattr(ctx, nil, out)
}

// Open opens the file and returns a handle for I/O.
func (f *fuseFile) Open(ctx context.Context, flags uint32) (fh fs.FileHandle, _ uint32, errno syscall.Errno) {
	defer fuseRecover(&errno)
	e, err := f.root.core.Lookup(f.path)
	if err != nil {
		return nil, 0, toFuseErr(f.path, err)
	}
	return newFuseFileHandle(f.root, f.path, e), 0, 0
}

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
	_ fs.FileReader   = (*fuseFileHandle)(nil)
	_ fs.FileWriter   = (*fuseFileHandle)(nil)
	_ fs.FileFlusher  = (*fuseFileHandle)(nil)
	_ fs.FileReleaser = (*fuseFileHandle)(nil)
)

func newFuseFileHandle(root *fuseRoot, p string, e *Entry) *fuseFileHandle {
	return &fuseFileHandle{root: root, path: p, mode: e.Mode}
}

// load reads the stored blob into data if not already loaded.
func (h *fuseFileHandle) load() error {
	if h.loaded {
		return nil
	}
	rc, e, err := h.root.core.Open(h.path)
	if err != nil {
		return err
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

// Read reads file data starting at the requested offset.
func (h *fuseFileHandle) Read(ctx context.Context, dest []byte, off int64) (res fuse.ReadResult, errno syscall.Errno) {
	defer fuseRecover(&errno)
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.load(); err != nil {
		return nil, toFuseErr(h.path, err)
	}
	if off >= int64(len(h.data)) {
		return fuse.ReadResultData(nil), 0
	}
	end := off + int64(len(dest))
	if end > int64(len(h.data)) {
		end = int64(len(h.data))
	}
	return fuse.ReadResultData(h.data[off:end]), 0
}

// Write writes data to the file at the requested offset.
func (h *fuseFileHandle) Write(ctx context.Context, data []byte, off int64) (written uint32, errno syscall.Errno) {
	defer fuseRecover(&errno)
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.load(); err != nil {
		return 0, toFuseErr(h.path, err)
	}
	need := off + int64(len(data))
	if need > int64(cap(h.data)) {
		newData := make([]byte, need)
		copy(newData, h.data)
		h.data = newData
	} else if need > int64(len(h.data)) {
		h.data = h.data[:need]
	}
	copy(h.data[off:], data)
	h.dirty = true
	return uint32(len(data)), 0
}

// flush materializes the buffered content into the store if dirty.
func (h *fuseFileHandle) flush() syscall.Errno {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.dirty {
		return 0
	}
	if _, _, err := h.root.core.Create(h.path, h.mode, bytes.NewReader(h.data)); err != nil {
		return toFuseErr(h.path, err)
	}
	h.dirty = false
	return 0
}

// Flush persists pending data for the open handle.
func (h *fuseFileHandle) Flush(ctx context.Context) (errno syscall.Errno) {
	defer fuseRecover(&errno)
	return h.flush()
}

// Release closes the open handle.
func (h *fuseFileHandle) Release(ctx context.Context) (errno syscall.Errno) {
	defer fuseRecover(&errno)
	return h.flush()
}

// ---- path helpers -----------------------------------------------------------

func joinPath(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return path.Join(dir, name)
}
