package storage

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"go.etcd.io/bbolt"
)

// Buckets used in Bbolt
var (
	bucketObjects    = []byte("objects")    // Maps ContentHash -> {ObjectMeta binary}
	bucketNamespace  = []byte("namespace")  // Maps FileName -> ContentHash
	bucketPaths      = []byte("paths")      // Maps FileName -> RemotePath
	bucketTombstones = []byte("tombstones") // Maps FileName -> deletion timestamp (unix nano)
	bucketModTimes   = []byte("modtimes")   // Maps FileName -> modification timestamp (unix nano)
	bucketS3Meta     = []byte("s3meta")     // Maps FileName -> JSON S3 object metadata (content-type, x-amz-meta-*)
	bucketQuarantine = []byte("quarantine") // Maps ContentHash -> mark-and-hold flag (R2, #930)
)

// ObjectMeta is the binary metadata stored in the objects bucket.
// Wire format: [8B size (big-endian)] [8B refCount (big-endian)] [8B deletedAt (big-endian)]
type ObjectMeta struct {
	Size      int64
	RefCount  int64
	DeletedAt int64 // unix nano; 0 = not deleted
}

// encodeObjectMeta serializes ObjectMeta into a fixed 24-byte binary slice.
func (m ObjectMeta) encode() []byte {
	buf := make([]byte, 24)
	binary.BigEndian.PutUint64(buf[0:8], uint64(m.Size))
	binary.BigEndian.PutUint64(buf[8:16], uint64(m.RefCount))
	binary.BigEndian.PutUint64(buf[16:24], uint64(m.DeletedAt))
	return buf
}

// decodeObjectMeta deserializes a 24-byte binary slice into ObjectMeta.
// Falls back to legacy ASCII size format for backward compatibility.
// A legacy value that cannot be parsed returns an error instead of silently
// degrading to a zero-size object (fix #640).
func decodeObjectMeta(val []byte) (ObjectMeta, error) {
	if len(val) == 24 {
		return ObjectMeta{
			Size:      int64(binary.BigEndian.Uint64(val[0:8])),
			RefCount:  int64(binary.BigEndian.Uint64(val[8:16])),
			DeletedAt: int64(binary.BigEndian.Uint64(val[16:24])),
		}, nil
	}
	// Legacy format: ASCII integer = size only, refCount=1, not deleted
	size, err := strconv.ParseInt(string(val), 10, 64)
	if err != nil {
		return ObjectMeta{}, fmt.Errorf("legacy object metadata parse failed for %q: %w", val, syscall.EBADMSG)
	}
	return ObjectMeta{Size: size, RefCount: 1}, nil
}

// Store defines the interface for object storage operations.
type Store interface {
	io.Closer
	Put(name string, hash string, size int64, remotePath string, content io.Reader) error
	Get(name string) (io.ReadCloser, common.FileMetadata, error)
	// GetMeta returns file metadata without opening the content stream. Query
	// paths that only need metadata (e.g. scatter-gather QueryGet) must use this
	// to avoid an unnecessary stream open on large blobs or remote S3 backends
	// (issue #660).
	GetMeta(name string) (common.FileMetadata, error)
	Has(hash string) (bool, error)
	GetHashForName(name string) (string, error)
	Delete(name string) error
	List() ([]common.FileMetadata, error)
}

// CASStore implements Content-Addressable Storage with Bbolt metadata.
// It composes a pluggable BlobStore (for raw blob bytes) with a fixed
// bbolt metadata layer (for refcounts, tombstones, namespace mappings).
type CASStore struct {
	mu        sync.RWMutex
	db        *bbolt.DB
	base      string
	blobs     BlobStore
	gcDone    chan struct{}
	gcWG      sync.WaitGroup
	closeOnce sync.Once
	gcOnce    sync.Once
	gcStarted atomic.Int32

	// VerifyOnRead re-derives the blob SHA-256 at read EOF and fails reads when
	// it no longer matches the content-address key (at-rest integrity, #924).
	VerifyOnRead bool

	// verifier is the rule-74 read-verification policy selected at construction
	// (everyReadVerifier by default; verifiedCache via WithReadVerifier).
	verifier ReadVerifier

	// durability is the R3 write-durability barrier (fsync default; group-
	// commit / none via WithDurability). nil preserves legacy per-blob fsync
	// inside the blob backend.
	durability DurabilityBarrier

	scrubDone    chan struct{}
	scrubWG      sync.WaitGroup
	scrubOnce    sync.Once
	scrubStarted atomic.Int32

	// Self-heal rebuild (R2, #930). Armed by StartRebuild; degradedRead enables
	// survivor-set read fallback in Get. rebuildSource is the Rule 74 cluster
	// seam; nil leaves degraded read and the rebuild loop inert.
	rebuildDone    chan struct{}
	rebuildWG      sync.WaitGroup
	rebuildOnce    sync.Once
	rebuildStarted atomic.Int32
	rebuildSource  RebuildSource
	rebuildTarget  int
	degradedRead   bool
	repairs        atomic.Uint64
}

// NewCASStore initializes a CAS store with a LocalBlobStore backend.
// This preserves backward compatibility with existing callers and tests.
func NewCASStore(dataDir string) (*CASStore, error) {
	blobs, err := NewLocalBlobStore(dataDir)
	if err != nil {
		return nil, err
	}
	return newCASStore(dataDir, blobs)
}

// newCASStore creates a CAS store with the given BlobStore backend and
// a bbolt metadata database in dataDir. Optional func(opts) configure
// behavior; the default read policy is everyReadVerifier (historical
// VerifyOnRead=true behavior).
func newCASStore(dataDir string, blobs BlobStore, opts ...func(*CASStore)) (*CASStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data dir: %v: %w", err, syscall.EIO)
	}

	dbPath := filepath.Join(dataDir, "momo.db")
	db, err := bbolt.Open(dbPath, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open bbolt: %v: %w", err, syscall.EIO)
	}

	// Initialize buckets
	err = db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketObjects); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketNamespace); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketPaths); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketModTimes); err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists(bucketTombstones)
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists(bucketS3Meta)
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists(bucketQuarantine)
		return err
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	s := &CASStore{
		db:           db,
		base:         dataDir,
		blobs:        blobs,
		gcDone:       make(chan struct{}),
		scrubDone:    make(chan struct{}),
		rebuildDone:  make(chan struct{}),
		VerifyOnRead: true,
		verifier:     everyReadVerifier{},
	}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

func (s *CASStore) Close() error {
	s.closeOnce.Do(func() {
		if s.gcDone != nil {
			close(s.gcDone)
			s.gcWG.Wait()
		}
		if s.scrubDone != nil {
			close(s.scrubDone)
			s.scrubWG.Wait()
		}
		if s.rebuildDone != nil {
			close(s.rebuildDone)
			s.rebuildWG.Wait()
		}
		s.mu.Lock()
		if s.blobs != nil {
			s.blobs.Close()
		}
		s.db.Close()
		s.mu.Unlock()
	})
	return nil
}

// Put saves an object to the store.
// If the hash already exists, it only updates the namespace mapping (deduplication).
func (s *CASStore) Put(name string, hash string, size int64, remotePath string, content io.Reader) (err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics in the storage backend.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in CASStore.Put for %s: %v", name, r)
			err = fmt.Errorf("internal storage panic: %w", syscall.EIO)
		}
	}()

	if common.HasPathTraversalChars(hash) {
		return fmt.Errorf("hash contains path traversal characters: %w", syscall.EINVAL)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Check if we already have the blob
	exists, err := s.hasInternal(hash)
	if err != nil {
		return fmt.Errorf("failed to check blob existence: %w", err)
	}
	if !exists && content != nil {
		if err := s.blobs.PutBlob(hash, content); err != nil {
			return err
		}
		// R3 durability barrier (fsync-before-ack): cross the configured
		// barrier before the write is acknowledged. A barrier failure fails
		// the write — no silent ack below the durability contract (#931).
		if s.durability != nil {
			if err := s.durability.Commit(hash); err != nil {
				return err
			}
		}
	} else if exists && content != nil {
		// Blob already exists, but we must still drain the content reader.
		// Callers (e.g., getFile) wrap content in a TeeReader to compute the
		// hash while streaming. If we skip reading, the TeeReader never reads
		// from the underlying connection, producing an empty hash and leaving
		// the payload data unconsumed on the wire.
		if _, err := io.Copy(io.Discard, content); err != nil {
			return fmt.Errorf("failed to drain content for existing blob: %w", err)
		}
	}

	// 2. Update Metadata
	return s.db.Update(func(tx *bbolt.Tx) error {
		ns := tx.Bucket(bucketNamespace)
		if err := ns.Put([]byte(name), []byte(hash)); err != nil {
			return fmt.Errorf("metadata error: %w", syscall.EIO)
		}

		obj := tx.Bucket(bucketObjects)
		// Update or create object metadata with reference counting.
		var meta ObjectMeta
		if existing := obj.Get([]byte(hash)); existing != nil {
			decoded, err := decodeObjectMeta(existing)
			if err != nil {
				return fmt.Errorf("failed to decode existing metadata for hash %s: %w", hash, err)
			}
			meta = decoded
			meta.RefCount++
		} else {
			meta = ObjectMeta{Size: size, RefCount: 1}
		}
		if err := obj.Put([]byte(hash), meta.encode()); err != nil {
			return fmt.Errorf("metadata error: %w", syscall.EIO)
		}

		// Remove any existing tombstone for this name (resurrection).
		if err := tx.Bucket(bucketTombstones).Delete([]byte(name)); err != nil {
			return fmt.Errorf("tombstone delete error: %w", err)
		}

		// Record modification time as Unix nanoseconds.
		var mtBuf [8]byte
		binary.BigEndian.PutUint64(mtBuf[:], uint64(time.Now().UnixNano()))
		if err := tx.Bucket(bucketModTimes).Put([]byte(name), mtBuf[:]); err != nil {
			return fmt.Errorf("metadata error: %w", syscall.EIO)
		}

		// Store RemotePath
		if len(name) > common.FileInfoLength {
			return fmt.Errorf("name exceeds maximum length: %w", syscall.ENAMETOOLONG)
		}
		if remotePath != "" {
			normalized, err := common.NormalizeVirtualPath(remotePath)
			if err != nil {
				return fmt.Errorf("invalid virtual path %q: %w", remotePath, err)
			}
			paths := tx.Bucket(bucketPaths)
			if err := paths.Put([]byte(name), []byte(normalized)); err != nil {
				return fmt.Errorf("metadata error: %w", syscall.EIO)
			}
		}
		return nil
	})
}

// Get retrieves an object by its human-readable name.
func (s *CASStore) Get(name string) (rc io.ReadCloser, meta common.FileMetadata, err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics during metadata parsing.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in CASStore.Get for %s: %v", name, r)
			err = fmt.Errorf("internal storage panic: %w", syscall.EIO)
		}
	}()

	var hash string
	err = func() error {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.db.View(func(tx *bbolt.Tx) error {
			// Check tombstone first — deleted names should appear as not found.
			if ts := tx.Bucket(bucketTombstones).Get([]byte(name)); ts != nil {
				return syscall.ENOENT
			}
			h := tx.Bucket(bucketNamespace).Get([]byte(name))
			if h == nil {
				return syscall.ENOENT
			}
			hash = string(h)
			return nil
		})
	}()
	if err != nil {
		return nil, common.FileMetadata{}, err
	}

	// Blob open (possibly a survivor-set degraded fetch, R2) happens with NO
	// lock held so a slow network repair does not block concurrent writers.
	f, blobErr := s.openVerifiedBlob(hash)
	if blobErr != nil {
		return nil, common.FileMetadata{}, blobErr
	}

	// 🛡️ Zero-Crash: Ensure file is closed if subsequent metadata lookups fail.
	defer func() {
		if err != nil {
			f.Close()
		}
	}()

	// Read metadata from DB in a single View (size, remotePath, modTime) to
	// minimize bbolt read-lock acquisition (perf: was 3 separate Views).
	// The blob open above intentionally stays outside any transaction so a
	// slow S3 download does not hold the bbolt read lock and block writers.
	var size int64
	var remotePath string
	var modTime int64
	err = func() error {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.db.View(func(tx *bbolt.Tx) error {
			val := tx.Bucket(bucketObjects).Get([]byte(hash))
			if val == nil {
				return fmt.Errorf("metadata missing for hash %s: %w", hash, syscall.ENOENT)
			}

			decoded, err := decodeObjectMeta(val)
			if err != nil {
				return err
			}
			meta := decoded
			size = meta.Size

			if size < 0 {
				return fmt.Errorf("invalid size %d for hash %s: %w", size, hash, syscall.EBADMSG)
			}

			if p := tx.Bucket(bucketPaths).Get([]byte(name)); p != nil {
				remotePath = string(p)
			}

			if mt := tx.Bucket(bucketModTimes).Get([]byte(name)); len(mt) >= 8 {
				modTime = int64(binary.BigEndian.Uint64(mt[:8]))
			}
			return nil
		})
	}()
	if err != nil {
		return nil, common.FileMetadata{}, err
	}

	return f, common.FileMetadata{Name: name, Hash: hash, Size: size, RemotePath: remotePath, ModTime: modTime}, nil
}

// GetMeta returns only the metadata for name without opening the content
// stream (issue #660). Query paths that only need metadata use this to avoid an
// unnecessary blob open on large objects or remote S3 backends.
func (s *CASStore) GetMeta(name string) (meta common.FileMetadata, err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics during metadata parsing.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in CASStore.GetMeta for %s: %v", name, r)
			err = fmt.Errorf("internal storage panic: %w", syscall.EIO)
		}
	}()

	s.mu.RLock()
	defer s.mu.RUnlock()

	var hash string
	err = s.db.View(func(tx *bbolt.Tx) error {
		// Check tombstone first — deleted names should appear as not found.
		if ts := tx.Bucket(bucketTombstones).Get([]byte(name)); ts != nil {
			return syscall.ENOENT
		}
		h := tx.Bucket(bucketNamespace).Get([]byte(name))
		if h == nil {
			return syscall.ENOENT
		}
		hash = string(h)
		return nil
	})
	if err != nil {
		return common.FileMetadata{}, err
	}

	// Read metadata from DB in a single View (size, remotePath, modTime).
	var size int64
	var remotePath string
	var modTime int64
	err = s.db.View(func(tx *bbolt.Tx) error {
		val := tx.Bucket(bucketObjects).Get([]byte(hash))
		if val == nil {
			return fmt.Errorf("metadata missing for hash %s: %w", hash, syscall.ENOENT)
		}

		decoded, err := decodeObjectMeta(val)
		if err != nil {
			return err
		}
		size = decoded.Size
		if size < 0 {
			return fmt.Errorf("invalid size %d for hash %s: %w", size, hash, syscall.EBADMSG)
		}

		if p := tx.Bucket(bucketPaths).Get([]byte(name)); p != nil {
			remotePath = string(p)
		}

		if mt := tx.Bucket(bucketModTimes).Get([]byte(name)); len(mt) >= 8 {
			modTime = int64(binary.BigEndian.Uint64(mt[:8]))
		}
		return nil
	})
	if err != nil {
		return common.FileMetadata{}, err
	}

	return common.FileMetadata{Name: name, Hash: hash, Size: size, RemotePath: remotePath, ModTime: modTime}, nil
}

// PutS3Meta persists optional S3 object headers (Content-Type, x-amz-meta-*,
// cache/encoding headers) at rest, keyed by object name. It is additive to the
// fixed FileMetadata fields and independent of the momo wire framing.
func (s *CASStore) PutS3Meta(name string, headers map[string]string) error {
	if len(headers) == 0 {
		return nil
	}
	data, err := common.MarshalS3MetaJSON(headers)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketS3Meta).Put([]byte(name), data)
	})
}

// VerifyChecksum reads the stored blob and verifies the given additive
// integrity checksums over it (opt-in bit-rot / stale-detection check, issue
// #903). It is bounded-memory (streams) and is a no-op when refs is empty.
// Returns an error wrapping common.ErrIntegrityMismatch on mismatch.
func (s *CASStore) VerifyChecksum(name string, refs []common.ChecksumRef) error {
	if len(refs) == 0 {
		return nil
	}
	rc, _, err := s.Get(name)
	if err != nil {
		return err
	}
	defer rc.Close()
	return common.VerifyStream(rc, refs)
}

// GetS3Meta returns the S3 object headers stored for name, or nil when none
// were recorded. Malformed payloads degrade to nil rather than failing reads.
func (s *CASStore) GetS3Meta(name string) map[string]string {
	var headers map[string]string
	_ = s.db.View(func(tx *bbolt.Tx) error {
		data := tx.Bucket(bucketS3Meta).Get([]byte(name))
		if len(data) == 0 {
			return nil
		}
		var err error
		headers, err = common.UnmarshalS3MetaJSON(data)
		if err != nil {
			log.Printf("AUDIT: failed to decode S3 metadata for %s: %v", name, err)
		}
		return nil
	})
	return headers
}

// Has checks if a content hash exists in the store.
func (s *CASStore) Has(hash string) (exists bool, err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in CASStore.Has for %s: %v", hash, r)
			err = fmt.Errorf("internal storage panic: %w", syscall.EIO)
		}
	}()

	if common.HasPathTraversalChars(hash) {
		return false, fmt.Errorf("hash contains path traversal characters: %w", syscall.EINVAL)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hasInternal(hash)
}

func (s *CASStore) hasInternal(hash string) (bool, error) {
	var exists bool
	err := s.db.View(func(tx *bbolt.Tx) error {
		val := tx.Bucket(bucketObjects).Get([]byte(hash))
		exists = (val != nil)
		return nil
	})
	return exists, err
}

// GetHashForName returns the content hash associated with the given name,
// or syscall.ENOENT if the name does not exist in the namespace.
// This is a lightweight metadata-only lookup that does not read the blob.
func (s *CASStore) GetHashForName(name string) (hash string, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in CASStore.GetHashForName for %s: %v", name, r)
			err = fmt.Errorf("internal storage panic: %w", syscall.EIO)
		}
	}()

	s.mu.RLock()
	defer s.mu.RUnlock()

	err = s.db.View(func(tx *bbolt.Tx) error {
		if ts := tx.Bucket(bucketTombstones).Get([]byte(name)); ts != nil {
			return syscall.ENOENT
		}
		h := tx.Bucket(bucketNamespace).Get([]byte(name))
		if h == nil {
			return syscall.ENOENT
		}
		hash = string(h)
		return nil
	})
	return
}

func (s *CASStore) Delete(name string) (err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in CASStore.Delete for %s: %v", name, r)
			err = fmt.Errorf("internal storage panic: %w", syscall.EIO)
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()

	var orphanedHash string
	now := time.Now().UnixNano()
	err = s.db.Update(func(tx *bbolt.Tx) error {
		ns := tx.Bucket(bucketNamespace)
		obj := tx.Bucket(bucketObjects)
		paths := tx.Bucket(bucketPaths)
		ts := tx.Bucket(bucketTombstones)

		// Write tombstone (8-byte unix nano timestamp).
		var tsBuf [8]byte
		binary.BigEndian.PutUint64(tsBuf[:], uint64(now))
		if err := ts.Put([]byte(name), tsBuf[:]); err != nil {
			return fmt.Errorf("metadata error: %w", syscall.EIO)
		}

		// Look up the hash for this name to decrement refcount.
		h := ns.Get([]byte(name))
		if h != nil {
			hash := string(h)
			if val := obj.Get([]byte(hash)); val != nil {
				decoded, err := decodeObjectMeta(val)
				if err != nil {
					return fmt.Errorf("failed to decode metadata for hash %s: %w", hash, err)
				}
				meta := decoded
				meta.RefCount--
				if meta.RefCount <= 0 {
					meta.RefCount = 0
					meta.DeletedAt = now
					orphanedHash = hash
				}
				if err := obj.Put([]byte(hash), meta.encode()); err != nil {
					return fmt.Errorf("metadata error: %w", syscall.EIO)
				}
			}
		}

		// Remove namespace and paths entries.
		if err := ns.Delete([]byte(name)); err != nil {
			return fmt.Errorf("namespace delete error: %w", err)
		}
		if err := paths.Delete([]byte(name)); err != nil {
			return fmt.Errorf("paths delete error: %w", err)
		}
		if err := tx.Bucket(bucketModTimes).Delete([]byte(name)); err != nil {
			return fmt.Errorf("modtime delete error: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 🛡️ CVE-006: Immediately delete blob content when refcount reaches 0.
	// Don't wait for GC — the blob is already orphaned. Blob deletion is
	// performed outside the bbolt transaction (same pattern as GC) to avoid
	// blocking all db operations during potential network I/O (S3 backends).
	if orphanedHash != "" {
		if delErr := s.blobs.DeleteBlob(orphanedHash); delErr != nil {
			log.Printf("AUDIT: Failed to delete orphaned blob %s: %v", orphanedHash, delErr)
		} else {
			if metaErr := s.db.Update(func(tx *bbolt.Tx) error {
				return tx.Bucket(bucketObjects).Delete([]byte(orphanedHash))
			}); metaErr != nil {
				log.Printf("AUDIT: Failed to remove metadata for orphaned blob %s: %v", orphanedHash, metaErr)
			}
		}
	}
	return nil
}

// List retrieves all file metadata entries in the store.
func (s *CASStore) List() (list []common.FileMetadata, err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in CASStore.List: %v", r)
			err = fmt.Errorf("internal storage panic: %w", syscall.EIO)
		}
	}()

	s.mu.RLock()
	defer s.mu.RUnlock()

	err = s.db.View(func(tx *bbolt.Tx) error {
		ns := tx.Bucket(bucketNamespace)
		if ns == nil {
			return nil
		}
		obj := tx.Bucket(bucketObjects)
		paths := tx.Bucket(bucketPaths)
		mtimes := tx.Bucket(bucketModTimes)
		ts := tx.Bucket(bucketTombstones)

		c := ns.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			name := string(k)
			hash := string(v)

			// Skip tombstoned entries.
			if ts != nil && ts.Get(k) != nil {
				continue
			}

			var size int64 = 0
			var remotePath string = ""
			var modTime int64 = 0

			if obj != nil {
				sizeBytes := obj.Get(v)
				if len(sizeBytes) > 0 {
					decoded, err := decodeObjectMeta(sizeBytes)
					if err != nil {
						return fmt.Errorf("failed to decode metadata for %s: %w", name, err)
					}
					size = decoded.Size
				}
			}

			if paths != nil {
				pBytes := paths.Get(k)
				if pBytes != nil {
					remotePath = string(pBytes)
				}
			}

			if mtimes != nil {
				mtBytes := mtimes.Get(k)
				if len(mtBytes) >= 8 {
					modTime = int64(binary.BigEndian.Uint64(mtBytes[:8]))
				}
			}

			list = append(list, common.FileMetadata{
				Name:       name,
				Hash:       hash,
				Size:       size,
				RemotePath: remotePath,
				ModTime:    modTime,
			})
		}
		return nil
	})

	return list, err
}

func (s *CASStore) GetBlobPath(name string) (path string, err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in CASStore.GetBlobPath for %s: %v", name, r)
			err = fmt.Errorf("internal storage panic: %w", syscall.EIO)
		}
	}()

	s.mu.RLock()
	defer s.mu.RUnlock()

	var hash string
	err = s.db.View(func(tx *bbolt.Tx) error {
		if ts := tx.Bucket(bucketTombstones).Get([]byte(name)); ts != nil {
			return syscall.ENOENT
		}
		h := tx.Bucket(bucketNamespace).Get([]byte(name))
		if h == nil {
			return syscall.ENOENT
		}
		hash = string(h)
		return nil
	})
	if err != nil {
		return "", err
	}

	// GetBlobPath exposes a local filesystem path for file serving. For
	// remote or raw-device backends no such path exists on the local disk
	// (fix #639).
	if !s.isLocalBackend() {
		return "", fmt.Errorf("GetBlobPath unsupported for non-local backend: %w", syscall.ENOTSUP)
	}

	return s.getBlobPath(hash), nil
}

// isLocalBackend reports whether blobs are stored on the local filesystem
// (including via the encryption decorator), for which getBlobPath is valid.
func (s *CASStore) isLocalBackend() bool {
	switch b := s.blobs.(type) {
	case *LocalBlobStore:
		return true
	case *EncryptedBlobStore:
		_, ok := b.inner.(*LocalBlobStore)
		return ok
	default:
		return false
	}
}

// getBlobPath transforms a hash into a tiered directory path.
// Hash "abcdef123..." -> "data/blobs/ab/cd/ef/abcdef123..."
func (s *CASStore) getBlobPath(hash string) string {
	if len(hash) < 6 {
		return filepath.Join(s.base, "blobs", hash)
	}
	return filepath.Join(s.base, "blobs", hash[0:2], hash[2:4], hash[4:6], hash)
}
