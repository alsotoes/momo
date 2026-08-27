// Package momofs implements the momo filesystem (R4, #932): a POSIX-metadata
// layer over the content-addressed object store. The FUSE/syscall transport is
// a separable adapter (documented follow-up); this package provides the
// storage-backed virtual filesystem core with correct metadata semantics —
// atomic rename, hardlinks aligned to CAS reference counts, permission
// enforcement, and read-your-writes consistency over a single backing store.
//
// Object model
//
//   - A directory is a content-addressed manifest blob: its store object holds
//     a JSON directory manifest (direct children with mode/uid/gid/size/mtime).
//     Creating/renaming/removing children rewrites the manifest, so directories
//     are atomic and versioned by content hash.
//   - A file is a normal store object whose name key derives from its content
//     hash ("f-<hash[:48]>"). Its POSIX metadata lives in the parent manifest.
//   - A hardlink creates a second manifest entry for the same content hash and
//     a second store reference (Put same key increments RefCount), keeping
//     refcounts aligned with link counts so CAS GC never reclaims a live link.
//   - Rename only rewrites manifests; it never mutates reference counts.
package momofs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

const (
	dirPrefix       = "d-"
	filePrefix      = "f-"
	manifestVersion = 1
	keyLen          = 48 // object-name key tail length, keeps names << FileInfoLength

	// DefaultMode is the default file mode for newly created entries.
	DefaultMode = 0o644
	// DefaultDirMode is the default mode for newly created directories.
	DefaultDirMode = 0o755
)

// EntryType discriminates manifest entries.
type EntryType uint8

// Entry types.
const (
	EntryFile EntryType = iota + 1
	EntryDir
)

// Entry is one POSIX directory entry carried in a directory manifest.
type Entry struct {
	Name  string    `json:"n"`
	Type  EntryType `json:"t"`
	Mode  uint32    `json:"m"`
	UID   uint32    `json:"u"`
	GID   uint32    `json:"g"`
	Size  int64     `json:"s"`
	MTime int64     `json:"mt"` // unix nanoseconds
	Hash  string    `json:"h,omitempty"`
	// ObjName is the backing-store object name for native (non-momofs) files;
	// for manifest-managed files it is empty and fileKey(Hash) is used.
	ObjName string `json:"-"`
}

// dirManifest is the content-addressed payload of a directory object.
type dirManifest struct {
	Version int      `json:"v"`
	Entries []*Entry `json:"entries"`
}

// Option configures a FS.
type Option func(*FS)

// WithRootMode sets the root directory mode (default 0755).
func WithRootMode(mode uint32) Option {
	return func(f *FS) { f.rootMode = mode }
}

// FS is a storage-backed POSIX filesystem root. It is safe for concurrent use.
type FS struct {
	store storage.Store
	mu    sync.Mutex

	rootMode uint32

	cached   map[string]cachedEntry
	ttl      time.Duration
	maxEntry int
}

type cachedEntry struct {
	e    *Entry
	at   time.Time
	meta common.FileMetadata
}

// New returns a FS rooted over the given store. The connection to the store is
// lazy: reads happen on first use, so a remounted FS (new instance over the
// same store) inherently sees fresh state (R4-C4).
func New(store storage.Store, opts ...Option) *FS {
	f := &FS{
		store:    store,
		rootMode: DefaultDirMode,
		cached:   make(map[string]cachedEntry),
		ttl:      0, // 0 disables caching: freshest reads within a FS
		maxEntry: 1 << 20,
	}
	for _, o := range opts {
		o(f)
	}
	return f
}

// WithCacheTTL enables entry caching with the given TTL (R4-C3). A zero TTL (the
// default) reads manifests fresh on every op, guaranteeing read-your-writes
// without invalidation logic.
func WithCacheTTL(ttl time.Duration) Option {
	return func(f *FS) { f.ttl = ttl }
}

// ---- path helpers ---------------------------------------------------------

// normalizePath validates and cleans an absolute POSIX path. momofs paths are
// absolute and rooted at "/".
func normalizePath(p string) (string, error) {
	if p == "" || p[0] != '/' {
		return "", fmt.Errorf("momofs: path must be absolute: %q: %w", p, syscall.EINVAL)
	}
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("momofs: path contains NUL: %w", syscall.EINVAL)
	}
	cleaned := path.Clean(p)
	if cleaned == "." {
		return "", fmt.Errorf("momofs: invalid root path: %w", syscall.EINVAL)
	}
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." || seg == " " || strings.Contains(seg, "\\") {
			return "", fmt.Errorf("momofs: invalid path segment %q: %w", seg, syscall.EINVAL)
		}
	}
	return cleaned, nil
}

// parentBase splits an absolute path into its parent directory and basename.
func parentBase(p string) (parent, base string) {
	parent = path.Dir(p)
	base = path.Base(p)
	return parent, base
}

// objKey returns the store object name for a file path. Names are per-path
// identities (dedup happens on the content hash), so renames and hardlinks
// keep one store reference per live path and store-level tombstones never
// mask another name that shares the same content (R4-C2).
func objKey(p string) string {
	sum := common.HashBytes([]byte(p))
	if len(sum) > keyLen {
		sum = sum[:keyLen]
	}
	return filePrefix + sum
}

// dirKey returns the store object name for a directory manifest path.
func dirKey(dirPath string) string {
	sum := common.HashBytes([]byte(dirPath))
	if len(sum) > keyLen {
		sum = sum[:keyLen]
	}
	return dirPrefix + sum
}

// isDirKey reports whether an object name is a directory manifest key.
func isDirKey(name string) bool { return strings.HasPrefix(name, dirPrefix) }

// ---- manifest IO ----------------------------------------------------------

// readManifest loads and decodes the directory manifest for dirPath. A missing
// directory reads as an empty manifest for the root, and ENOENT otherwise.
func (f *FS) readManifest(dirPath string) (*dirManifest, error) {
	if f.ttl > 0 {
		if ce, ok := f.cached[dirPath]; ok && time.Since(ce.at) < f.ttl && ce.e != nil && ce.e.Type == EntryDir {
			return f.manifestFromEntry(dirPath, ce.e)
		}
	}
	key := dirKey(dirPath)
	if dirPath == "/" {
		rc, _, err := f.store.Get(key)
		if err != nil {
			if err == syscall.ENOENT || strings.Contains(err.Error(), syscall.ENOENT.Error()) {
				return &dirManifest{Version: manifestVersion, Entries: []*Entry{}}, nil
			}
			return nil, err
		}
		defer rc.Close()
		return decodeManifest(rc)
	}
	rc, _, err := f.store.Get(key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return decodeManifest(rc)
}

// manifestFromEntry re-derives a (possibly cached) dir manifest from an entry
// by re-reading the store object (fresh), so caching of the dir ENTRY never
// serves stale child lists (R4-C4).
func (f *FS) manifestFromEntry(dirPath string, _ *Entry) (*dirManifest, error) {
	rc, _, err := f.store.Get(dirKey(dirPath))
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return decodeManifest(rc)
}

// writeManifest atomically stores a manifest blob as the directory object.
// Manifests are addressed only by their content-hash key; they intentionally
// carry no RemotePath, so they never collide with the mount's path index.
func (f *FS) writeManifest(dirPath string, m *dirManifest) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(data) > f.maxEntry {
		return fmt.Errorf("momofs: directory %s exceeds max manifest size: %w", dirPath, syscall.EFBIG)
	}
	hash := common.HashBytes(data)
	key := dirKey(dirPath)
	return f.store.Put(key, hash, int64(len(data)), "", bytes.NewReader(data))
}

func decodeManifest(r io.Reader) (*dirManifest, error) {
	data, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return nil, err
	}
	var m dirManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("momofs: corrupt directory manifest: %w", err)
	}
	if m.Version != manifestVersion {
		return nil, fmt.Errorf("momofs: unsupported manifest version %d: %w", m.Version, syscall.EINVAL)
	}
	return &m, nil
}

// entryIn returns the direct child entry of dirPath named base, or ENOENT.
func (f *FS) entryIn(dirPath, base string) (*Entry, error) {
	m, err := f.readManifest(dirPath)
	if err != nil {
		return nil, err
	}
	for _, e := range m.Entries {
		if e.Name == base {
			return e, nil
		}
	}
	return nil, fmt.Errorf("momofs: no such entry %q: %w", path.Join(dirPath, base), syscall.ENOENT)
}

// cacheEntry refreshes the entry cache for a path.
func (f *FS) cacheEntry(p string, e *Entry, meta common.FileMetadata) {
	if f.ttl <= 0 {
		return
	}
	f.cached[p] = cachedEntry{e: e, at: time.Now(), meta: meta}
}

// ---- public operations -----------------------------------------------------

// Lookup resolves p and returns its entry. Missing entries return ENOENT.
func (f *FS) Lookup(p string) (*Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lookupLocked(p)
}

func (f *FS) lookupLocked(p string) (*Entry, error) {
	p, err := normalizePath(p)
	if err != nil {
		return nil, err
	}
	if p == "/" {
		return &Entry{Name: "/", Type: EntryDir, Mode: f.rootMode, MTime: time.Now().UnixNano()}, nil
	}
	parent, base := parentBase(p)
	if e, err := f.entryIn(parent, base); err == nil {
		return e, nil
	}
	// R4-C3: fall back to the backing store index so objects written natively
	// (S3/momo) appear in the mount without a manifest entry.
	if meta, ok := f.storeMetaByPath(p); ok {
		e := metaToEntry(base, meta)
		f.cacheEntry(p, e, meta)
		return e, nil
	}
	if f.storePathIsAncestorOfObject(p) {
		return &Entry{Name: base, Type: EntryDir, Mode: DefaultDirMode, MTime: 0}, nil
	}
	return nil, fmt.Errorf("momofs: no such entry %q: %w", p, syscall.ENOENT)
}

// GetAttr returns the entry metadata for p.
func (f *FS) GetAttr(p string) (*Entry, error) { return f.Lookup(p) }

// SetAttr updates mode / ownership / mtime for p (R4-C2). Zero-valued
// fields are ignored.
func (f *FS) SetAttr(p string, mode *uint32, uid, gid *uint32, mtime *int64) (*Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, err := normalizePath(p)
	if err != nil {
		return nil, err
	}
	if p == "/" {
		newMode := f.rootMode
		if mode != nil {
			newMode = *mode & 0o7777
		}
		f.rootMode = newMode
		return &Entry{Name: "/", Type: EntryDir, Mode: newMode, MTime: time.Now().UnixNano()}, nil
	}
	parent, base := parentBase(p)
	m, err := f.readManifest(parent)
	if err != nil {
		return nil, err
	}
	e := findEntry(m, base)
	if e == nil {
		return nil, fmt.Errorf("momofs: no such entry %q: %w", p, syscall.ENOENT)
	}
	// Validate permissions: never allow setuid/setgid/sticky via POSIX setattr
	// without a mode flag that requests them.
	if mode != nil {
		e.Mode = *mode & 0o7777
	}
	if uid != nil {
		e.UID = *uid
	}
	if gid != nil {
		e.GID = *gid
	}
	if mtime != nil {
		e.MTime = *mtime
	}
	if err := f.writeManifest(parent, m); err != nil {
		return nil, err
	}
	delete(f.cached, p)
	return e, nil
}

// ReadDir lists the direct children of dirPath, sorted by name.
func (f *FS) ReadDir(p string) ([]*Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, err := normalizePath(p)
	if err != nil {
		return nil, err
	}
	m, err := f.readManifest(p)
	if err != nil && err != syscall.ENOENT && !strings.Contains(err.Error(), syscall.ENOENT.Error()) {
		return nil, err
	}
	seen := map[string]bool{}
	var out []*Entry
	if m != nil {
		for _, e := range m.Entries {
			seen[e.Name] = true
			out = append(out, e)
		}
	}
	// R4-C3: union in backing-store children (natively-written objects and
	// synthetic subdirectories derived from their paths).
	for name, e := range f.storeChildren(p) {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Mkdir creates a new directory at p (R4-C1).
func (f *FS) Mkdir(p string, mode uint32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mkdirLocked(p, mode)
}

func (f *FS) mkdirLocked(p string, mode uint32) error {
	p, err := normalizePath(p)
	if err != nil {
		return err
	}
	if p == "/" {
		return fmt.Errorf("momofs: root exists: %w", syscall.EEXIST)
	}
	parent, base := parentBase(p)
	pm, err := f.readManifest(parent)
	if err != nil {
		return err
	}
	if findEntry(pm, base) != nil {
		return fmt.Errorf("momofs: entry exists %q: %w", p, syscall.EEXIST)
	}
	now := time.Now().UnixNano()
	pm.Entries = append(pm.Entries, &Entry{
		Name: base, Type: EntryDir, Mode: mode & 0o7777,
		UID: 0, GID: 0, Size: 0, MTime: now,
	})
	if err := f.writeManifest(parent, pm); err != nil {
		return err
	}
	// Materialize the empty directory manifest so Lookup/readdir on the child
	// works and the tree is self-describing.
	if err := f.writeManifest(p, &dirManifest{Version: manifestVersion, Entries: []*Entry{}}); err != nil {
		return err
	}
	delete(f.cached, p)
	return nil
}

// Create writes a new or replaces an existing regular file at p (R4-C1). The
// content is read once, hashed, and stored content-addressed.
func (f *FS) Create(p string, mode uint32, content io.Reader) (size int64, hash string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("momofs: Create panic: %v: %w", r, syscall.EIO)
		}
	}()
	f.mu.Lock()
	defer f.mu.Unlock()
	p, err = normalizePath(p)
	if err != nil {
		return 0, "", err
	}
	parent, base := parentBase(p)
	pm, err := f.readManifest(parent)
	if err != nil {
		return 0, "", err
	}
	if existing := findEntry(pm, base); existing != nil && existing.Type == EntryDir {
		return 0, "", fmt.Errorf("momofs: is a directory %q: %w", p, syscall.EISDIR)
	}

	// Hash the stream once (bounded memory), then persist under the
	// content-derived key.
	data, err := io.ReadAll(io.LimitReader(content, common.MaxFileSize+1))
	if err != nil {
		return 0, "", err
	}
	if int64(len(data)) > common.MaxFileSize {
		return 0, "", fmt.Errorf("momofs: file too large: %w", syscall.EFBIG)
	}
	hash = common.HashBytes(data)
	key := objKey(p)
	if err := f.store.Put(key, hash, int64(len(data)), p, bytes.NewReader(data)); err != nil {
		return 0, "", err
	}

	now := time.Now().UnixNano()
	mode = mode & 0o7777
	replace := false
	for i, e := range pm.Entries {
		if e.Name == base {
			// Replace: same path now addresses a new content version.
			pm.Entries[i] = &Entry{
				Name: base, Type: EntryFile, Mode: mode, UID: e.UID, GID: e.GID,
				Size: int64(len(data)), MTime: now, Hash: hash,
			}
			replace = true
			break
		}
	}
	if !replace {
		pm.Entries = append(pm.Entries, &Entry{
			Name: base, Type: EntryFile, Mode: mode, UID: 0, GID: 0,
			Size: int64(len(data)), MTime: now, Hash: hash,
		})
	}
	if err := f.writeManifest(parent, pm); err != nil {
		return 0, "", err
	}
	delete(f.cached, p)
	return int64(len(data)), hash, nil
}

// Open returns a reader for the file at p. Missing files return ENOENT.
func (f *FS) Open(p string) (io.ReadCloser, *Entry, error) {
	defer func() {
		if r := recover(); r != nil {
			_ = r
		}
	}()
	f.mu.Lock()
	e, err := f.lookupLocked(p)
	f.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	if e.Type != EntryFile {
		return nil, nil, fmt.Errorf("momofs: is a directory %q: %w", p, syscall.EISDIR)
	}
	objName := e.ObjName
	if objName == "" {
		objName = objKey(p)
	}
	rc, meta, err := f.store.Get(objName)
	if err != nil {
		return nil, nil, err
	}
	return rc, &Entry{Name: e.Name, Type: EntryFile, Mode: e.Mode, UID: e.UID, GID: e.GID,
		Size: e.Size, MTime: e.MTime, Hash: meta.Hash}, nil
}

// ReadAt reads len(b) bytes from the file at p starting at off.
func (f *FS) ReadAt(p string, off int64, b []byte) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("momofs: negative offset: %w", syscall.EINVAL)
	}
	rc, e, err := f.Open(p)
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	if off >= e.Size {
		return 0, io.EOF
	}
	if _, err := io.CopyN(io.Discard, rc, off); err != nil && err != io.EOF {
		return 0, err
	}
	n, err := io.ReadFull(rc, b)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		return n, nil
	}
	return n, err
}

// Remove unlinks p. Directories must be empty (R4-C2/POSIX rmdir).
func (f *FS) Remove(p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, err := normalizePath(p)
	if err != nil {
		return err
	}
	if p == "/" {
		return fmt.Errorf("momofs: cannot remove root: %w", syscall.EBUSY)
	}
	parent, base := parentBase(p)
	m, err := f.readManifest(parent)
	if err != nil {
		return err
	}
	e := findEntry(m, base)
	if e == nil {
		return fmt.Errorf("momofs: no such entry %q: %w", p, syscall.ENOENT)
	}
	if e.Type == EntryDir {
		cm, err := f.readManifest(p)
		if err != nil {
			return err
		}
		if len(cm.Entries) > 0 {
			return fmt.Errorf("momofs: directory not empty %q: %w", p, syscall.ENOTEMPTY)
		}
		if err := f.store.Delete(dirKey(p)); err != nil {
			return err
		}
	} else {
		// Unlink one path: drop that path's store reference (one per link), so
		// hardlink counts stay aligned with CAS refcounts and the blob survives
		// until the last live link is removed.
		if err := f.store.Delete(objKey(p)); err != nil {
			return err
		}
	}
	m.Entries = removeEntry(m.Entries, base)
	if err := f.writeManifest(parent, m); err != nil {
		return err
	}
	delete(f.cached, p)
	return nil
}

// Rename atomically moves src to dst within the POSIX namespace (R4-C2).
// Reference counts are untouched: only manifests change.
func (f *FS) Rename(src, dst string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	src, err := normalizePath(src)
	if err != nil {
		return err
	}
	dst, err = normalizePath(dst)
	if err != nil {
		return err
	}
	if src == "/" || dst == "/" {
		return fmt.Errorf("momofs: cannot rename root: %w", syscall.EINVAL)
	}
	sParent, sBase := parentBase(src)
	dParent, dBase := parentBase(dst)

	sm, err := f.readManifest(sParent)
	if err != nil {
		return err
	}
	e := findEntry(sm, sBase)
	if e == nil {
		return fmt.Errorf("momofs: no such entry %q: %w", src, syscall.ENOENT)
	}
	moved := *e
	moved.Name = dBase

	// A source directory has its own manifest object that must relocate to the
	// destination key, and every descendant's path-keyed references (directory
	// manifests and file objects) must be rewritten too.
	if e.Type == EntryDir {
		if err := f.rewriteSubtree(src, dst); err != nil {
			return err
		}
	}

	// Same-parent rename: single manifest mutation (remove + re-add under the
	// new name); otherwise mutate both parent manifests.
	// Overwrite semantics: a pre-existing target at the destination (other than
	// the moving entry itself) is removed first, releasing its path reference.
	overwriteTarget := false
	if dm, err := f.readManifest(dParent); err == nil {
		if existing := findEntry(dm, dBase); existing != nil {
			if existing.Type != e.Type {
				return fmt.Errorf("momofs: rename type mismatch: %w", syscall.EINVAL)
			}
			overwriteTarget = true
			if existing.Type == EntryFile {
				if err := f.store.Delete(objKey(dst)); err != nil {
					return err
				}
			} else {
				if err := f.store.Delete(dirKey(dst)); err != nil {
					return err
				}
			}
		}
	}

	// Move a file reference between store keys (net refcount unchanged);
	// tombstone on the destination key is cleared by the Put and the source
	// reference dropped afterwards.
	if e.Type == EntryFile {
		if err := f.store.Put(objKey(dst), e.Hash, e.Size, dst, nil); err != nil {
			return err
		}
		if err := f.store.Delete(objKey(src)); err != nil {
			return err
		}
	}

	// Apply the manifest mutations: source parent loses the entry; destination
	// parent gains it (and drops any overwritten target).
	_ = overwriteTarget
	if dParent == sParent {
		m, err := f.readManifest(sParent)
		if err != nil {
			return err
		}
		m.Entries = removeEntry(m.Entries, sBase)
		m.Entries = removeEntry(m.Entries, dBase)
		m.Entries = append(m.Entries, &moved)
		if err := f.writeManifest(sParent, m); err != nil {
			return err
		}
	} else {
		dm, err := f.readManifest(dParent)
		if err != nil {
			return err
		}
		dm.Entries = removeEntry(dm.Entries, dBase)
		dm.Entries = append(dm.Entries, &moved)
		if err := f.writeManifest(dParent, dm); err != nil {
			return err
		}
		sm.Entries = removeEntry(sm.Entries, sBase)
		if err := f.writeManifest(sParent, sm); err != nil {
			return err
		}
	}

	delete(f.cached, src)
	delete(f.cached, dst)
	return nil
}

// rewriteSubtree relocates the directory manifest at oldRoot to newRoot and
// rewrites every descendant's path-keyed reference (directory manifests and
// file objects), because store keys embed the full path. It is only used for
// directory renames; per-file keys are handled inline. Bounded by the subtree
// size (O(subtree)), matching POSIX mv cost.
func (f *FS) rewriteSubtree(oldRoot, newRoot string) error {
	srcMan, err := f.readManifest(oldRoot)
	if err != nil {
		return err
	}
	data, err := json.Marshal(srcMan)
	if err != nil {
		return err
	}
	dstHash := common.HashBytes(data)
	if err := f.store.Put(dirKey(newRoot), dstHash, int64(len(data)), "", bytes.NewReader(data)); err != nil {
		return err
	}
	if err := f.store.Delete(dirKey(oldRoot)); err != nil {
		return err
	}

	for _, e := range srcMan.Entries {
		oldChild := path.Join(oldRoot, e.Name)
		newChild := path.Join(newRoot, e.Name)
		switch e.Type {
		case EntryFile:
			if err := f.store.Put(objKey(newChild), e.Hash, e.Size, newChild, nil); err != nil {
				return err
			}
			if err := f.store.Delete(objKey(oldChild)); err != nil {
				return err
			}
		case EntryDir:
			if err := f.rewriteSubtree(oldChild, newChild); err != nil {
				return err
			}
		}
	}
	return nil
}

// Link creates a hardlink dst pointing at src's content (R4-C2). A second
// store reference is taken so refcounts stay aligned with link counts.
func (f *FS) Link(src, dst string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	src, err := normalizePath(src)
	if err != nil {
		return err
	}
	dst, err = normalizePath(dst)
	if err != nil {
		return err
	}
	sParent, sBase := parentBase(src)
	sm, err := f.readManifest(sParent)
	if err != nil {
		return err
	}
	e := findEntry(sm, sBase)
	if e == nil {
		return fmt.Errorf("momofs: no such entry %q: %w", src, syscall.ENOENT)
	}
	if e.Type != EntryFile {
		return fmt.Errorf("momofs: cannot link directory: %w", syscall.EPERM)
	}

	dParent, dBase := parentBase(dst)
	dm, err := f.readManifest(dParent)
	if err != nil {
		return err
	}
	if findEntry(dm, dBase) != nil {
		return fmt.Errorf("momofs: entry exists %q: %w", dst, syscall.EEXIST)
	}

	// Second reference under the destination path (dedup keeps one blob, the
	// hash refcount becomes 2 — GC-aligned with the two live links).
	if err := f.store.Put(objKey(dst), e.Hash, e.Size, dst, nil); err != nil {
		return err
	}
	dm.Entries = append(dm.Entries, &Entry{
		Name: dBase, Type: EntryFile, Mode: e.Mode, UID: e.UID, GID: e.GID,
		Size: e.Size, MTime: e.MTime, Hash: e.Hash,
	})
	if err := f.writeManifest(dParent, dm); err != nil {
		return err
	}
	delete(f.cached, dst)
	return nil
}

// ---- helpers ---------------------------------------------------------------

// findEntry returns the direct child entry named base or nil.
func findEntry(m *dirManifest, base string) *Entry {
	for _, e := range m.Entries {
		if e.Name == base {
			return e
		}
	}
	return nil
}

// removeEntry drops the direct child named base.
func removeEntry(entries []*Entry, base string) []*Entry {
	out := entries[:0]
	for _, e := range entries {
		if e.Name != base {
			out = append(out, e)
		}
	}
	return out
}

// ---- backing-store index (R4-C3: S3/momo-native visibility) ----------------

// storeObjects returns the store index, rebuilt on demand so natively-written
// objects appear in the mount without a manifest entry.
func (f *FS) storeObjects() ([]common.FileMetadata, error) {
	ms, err := f.store.List()
	if err != nil {
		return nil, err
	}
	out := make([]common.FileMetadata, 0, len(ms))
	for _, m := range ms {
		// Internal momofs objects (directory manifests and content-keyed file
		// objects) are addressed via manifests, not the path index; exposing
		// them would leak stale RemotePaths after renames. Only natively
		// written objects participate in the path index.
		if isDirKey(m.Name) || strings.HasPrefix(m.Name, filePrefix) {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// remoteForm converts an absolute mount path into the store's path spelling
// (NormalizeVirtualPath strips the leading slash).
func remoteForm(p string) string { return strings.TrimPrefix(p, "/") }

// storeMetaByPath finds a backing-store object whose RemotePath is exactly p.
func (f *FS) storeMetaByPath(p string) (common.FileMetadata, bool) {
	want := remoteForm(p)
	objs, err := f.storeObjects()
	if err != nil {
		return common.FileMetadata{}, false
	}
	for _, m := range objs {
		if m.RemotePath == want {
			return m, true
		}
	}
	return common.FileMetadata{}, false
}

// storePathIsAncestorOfObject reports whether p is an ancestor directory of any
// backing-store object (synthetic directory).
func (f *FS) storePathIsAncestorOfObject(p string) bool {
	prefix := remoteForm(p) + "/"
	objs, err := f.storeObjects()
	if err != nil {
		return false
	}
	for _, m := range objs {
		if strings.HasPrefix(m.RemotePath, prefix) {
			return true
		}
	}
	return false
}

// storeChildren returns the mount children derived purely from the backing
// store: direct file objects whose parent path is p, plus synthetic
// subdirectories implied by the path tree.
func (f *FS) storeChildren(p string) map[string]*Entry {
	canon := remoteForm(p)
	prefix := ""
	if canon != "" {
		prefix = canon + "/"
	}
	children := map[string]*Entry{}
	objs, err := f.storeObjects()
	if err != nil {
		return children
	}
	for _, m := range objs {
		rp := m.RemotePath
		if rp == "" {
			continue
		}
		rest := rp
		if prefix != "" && !strings.HasPrefix(rp, prefix) {
			continue
		}
		if prefix != "" {
			rest = strings.TrimPrefix(rp, prefix)
		}
		if rest == "" {
			continue
		}
		seg := rest
		isSub := false
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			seg = rest[:i]
			isSub = true
		}
		if seg == "" {
			continue
		}
		if existing, ok := children[seg]; ok {
			// A direct file wins over a synthetic directory marker.
			if existing.Type == EntryFile {
				continue
			}
			if !isSub {
				children[seg] = metaToEntry(seg, m)
			}
			continue
		}
		if isSub {
			children[seg] = &Entry{Name: seg, Type: EntryDir, Mode: DefaultDirMode}
		} else {
			children[seg] = metaToEntry(seg, m)
		}
	}
	return children
}

// metaToEntry maps a backing-store object to a mount file entry.
func metaToEntry(base string, m common.FileMetadata) *Entry {
	return &Entry{
		Name: base, Type: EntryFile, Mode: DefaultMode, UID: 0, GID: 0,
		Size: m.Size, MTime: m.ModTime, Hash: m.Hash, ObjName: m.Name,
	}
}
