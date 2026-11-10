package storage

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"go.etcd.io/bbolt"
)

// Buckets used in Bbolt
var (
	bucketObjects        = []byte("objects")           // Maps ContentHash -> {ObjectMeta binary}
	bucketNamespace      = []byte("namespace")         // Maps FileName -> ContentHash
	bucketPaths          = []byte("paths")             // Maps FileName -> RemotePath
	bucketTombstones     = []byte("tombstones")        // Maps FileName -> deletion timestamp (unix nano)
	bucketModTimes       = []byte("modtimes")          // Maps FileName -> modification timestamp (unix nano)
	bucketS3Meta         = []byte("s3meta")            // Maps FileName -> JSON S3 object metadata (content-type, x-amz-meta-*)
	bucketQuarantine     = []byte("quarantine")        // Maps ContentHash -> mark-and-hold flag (R2, #930)
	bucketTenantMeta     = []byte("tenant_meta")       // Maps TenantID -> JSON TenantConfig (R8, #936)
	bucketAuditLog       = []byte("audit_log")         // Immutable append-only audit log entries (R8, #936)
	bucketACL            = []byte("acl")               // Bucket/Object ACLs (R8, #936)
	bucketVersioning     = []byte("versioning")        // Maps bucket -> versioning status (R10, #938)
	bucketObjectVersions = []byte("object_versions")   // Maps bucket\x00key\x00versionID -> VersionRecord (R10, #938)
	bucketLifecycle      = []byte("lifecycle")         // Maps bucket -> JSON LifecycleConfig (R10, #938)
	bucketNotification   = []byte("notification")      // Maps bucket -> JSON NotificationConfig (R10, #938)
	bucketObjectLockCfg  = []byte("object_lock")       // Maps bucket -> lock-enabled flag (R10-LK1, #938)
	bucketAuditMeta      = []byte("audit_meta")        // Audit retention anchor: hash of the last pruned entry (R8, #1307)
	bucketLockState      = []byte("object_lock_state") // Maps bucket\x00key\x00versionID -> ObjectLockState JSON (R10-LK1, #938)
	bucketMetadataHints  = []byte("metadata_hints")    // Maps peer+name -> pending metadata record (R6 v2 hinted handoff)
)

// ObjectMeta is the binary metadata stored in the objects bucket.
// Wire format v2 (24 bytes legacy + variable):
//
//	[8B size] [8B refCount] [8B deletedAt] [4B vectorClockLen] [vectorClock...] [4B shardKeyLen] [shardKey...] [4B replicaCount] [replica1...replicaN]
//
// Legacy format (24 bytes): [8B size] [8B refCount] [8B deletedAt]
type ObjectMeta struct {
	Size             int64
	RefCount         int64
	DeletedAt        int64    // unix nano; 0 = not deleted
	VectorClock      []uint64 // per-node logical clock for conflict detection
	ShardKey         string   // consistent hash ring shard key
	MetadataReplicas []int32  // node IDs holding metadata replicas
}

// encodeObjectMeta serializes ObjectMeta into a binary slice.
func (m ObjectMeta) encode() []byte {
	// Legacy fixed part
	buf := make([]byte, 24)
	binary.BigEndian.PutUint64(buf[0:8], uint64(m.Size))
	binary.BigEndian.PutUint64(buf[8:16], uint64(m.RefCount))
	binary.BigEndian.PutUint64(buf[16:24], uint64(m.DeletedAt))

	// Vector clock
	vcLen := len(m.VectorClock)
	vcBuf := make([]byte, 4+8*vcLen)
	binary.BigEndian.PutUint32(vcBuf[0:4], uint32(vcLen))
	for i, v := range m.VectorClock {
		binary.BigEndian.PutUint64(vcBuf[4+8*i:4+8*(i+1)], v)
	}
	buf = append(buf, vcBuf...)

	// Shard key: 4-byte length prefix, then the raw bytes. The length header
	// must be exactly 4 bytes — allocating it with the payload size appended
	// len(shardKey) zero bytes and corrupted every following field.
	skBytes := []byte(m.ShardKey)
	skLenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(skLenBuf[0:4], uint32(len(skBytes)))
	buf = append(buf, skLenBuf...)
	buf = append(buf, skBytes...)

	// Metadata replicas
	mrLen := len(m.MetadataReplicas)
	mrBuf := make([]byte, 4+4*mrLen)
	binary.BigEndian.PutUint32(mrBuf[0:4], uint32(mrLen))
	for i, v := range m.MetadataReplicas {
		binary.BigEndian.PutUint32(mrBuf[4+4*i:4+4*(i+1)], uint32(v))
	}
	buf = append(buf, mrBuf...)

	return buf
}

// decodeObjectMeta deserializes a binary slice into ObjectMeta.
// Supports legacy ASCII format (size only), legacy 24-byte binary format,
// and v2 format with vector clock, shard key, and replicas.
func decodeObjectMeta(val []byte) (ObjectMeta, error) {
	if len(val) == 24 {
		// Legacy binary format: size/refCount/deletedAt only
		return ObjectMeta{
			Size:      int64(binary.BigEndian.Uint64(val[0:8])),
			RefCount:  int64(binary.BigEndian.Uint64(val[8:16])),
			DeletedAt: int64(binary.BigEndian.Uint64(val[16:24])),
		}, nil
	}
	// Legacy ASCII format: size as decimal string (e.g., "6")
	// This handles the pre-binary format from very old versions
	size, err := strconv.ParseInt(string(val), 10, 64)
	if err == nil {
		return ObjectMeta{Size: size, RefCount: 1}, nil
	}

	// New v2 format with vector clock, shard key, replicas
	if len(val) < 24 {
		return ObjectMeta{}, fmt.Errorf("object metadata too short (%d bytes): %w", len(val), syscall.EBADMSG)
	}
	m := ObjectMeta{
		Size:      int64(binary.BigEndian.Uint64(val[0:8])),
		RefCount:  int64(binary.BigEndian.Uint64(val[8:16])),
		DeletedAt: int64(binary.BigEndian.Uint64(val[16:24])),
	}
	offset := 24

	// Vector clock
	if len(val) < offset+4 {
		return ObjectMeta{}, fmt.Errorf("truncated vector clock length: %w", syscall.EBADMSG)
	}
	vcLen := int(binary.BigEndian.Uint32(val[offset : offset+4]))
	offset += 4
	if len(val) < offset+8*vcLen {
		return ObjectMeta{}, fmt.Errorf("truncated vector clock data: %w", syscall.EBADMSG)
	}
	m.VectorClock = make([]uint64, vcLen)
	for i := 0; i < vcLen; i++ {
		m.VectorClock[i] = binary.BigEndian.Uint64(val[offset : offset+8])
		offset += 8
	}

	// Shard key
	if len(val) < offset+4 {
		return ObjectMeta{}, fmt.Errorf("truncated shard key length: %w", syscall.EBADMSG)
	}
	skLen := int(binary.BigEndian.Uint32(val[offset : offset+4]))
	offset += 4
	if len(val) < offset+skLen {
		return ObjectMeta{}, fmt.Errorf("truncated shard key data: %w", syscall.EBADMSG)
	}
	m.ShardKey = string(val[offset : offset+skLen])
	offset += skLen

	// Metadata replicas
	if len(val) < offset+4 {
		return ObjectMeta{}, fmt.Errorf("truncated replica count: %w", syscall.EBADMSG)
	}
	mrLen := int(binary.BigEndian.Uint32(val[offset : offset+4]))
	offset += 4
	if len(val) < offset+4*mrLen {
		return ObjectMeta{}, fmt.Errorf("truncated replica data: %w", syscall.EBADMSG)
	}
	m.MetadataReplicas = make([]int32, mrLen)
	for i := 0; i < mrLen; i++ {
		m.MetadataReplicas[i] = int32(binary.BigEndian.Uint32(val[offset : offset+4]))
		offset += 4
	}

	return m, nil
}

// Store defines the interface for object storage operations.
type Store interface {
	io.Closer
	// Put stores a blob and its metadata (name, hash, size, remote path) under
	// the given name, reading the content from the provided reader.
	Put(name string, hash string, size int64, remotePath string, content io.Reader) error
	// Get retrieves the blob content stream and metadata for the given name.
	Get(name string) (io.ReadCloser, common.FileMetadata, error)
	// GetMeta returns file metadata without opening the content stream. Query
	// paths that only need metadata (e.g. scatter-gather QueryGet) must use this
	// to avoid an unnecessary stream open on large blobs or remote S3 backends
	// (issue #660).
	GetMeta(name string) (common.FileMetadata, error)
	// Has reports whether a blob with the given content hash is already stored.
	Has(hash string) (bool, error)
	// GetHashForName returns the content hash recorded for the given name.
	GetHashForName(name string) (string, error)
	// Delete removes the blob and its metadata for the given name.
	Delete(name string) error
	// List returns metadata for all stored objects.
	List() ([]common.FileMetadata, error)
	// WriteAuditLog appends an audit log entry with hash chaining.
	WriteAuditLog(entry *common.AuditLogEntry) error
	// GetTenantIDByAuthToken returns the tenant ID associated with the given auth token.
	GetTenantIDByAuthToken(authToken string) (string, error)
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

	// verHook replicates locally recorded object versions to peers (R10
	// follow-up, #1233). Guarded by verHookMu; nil disables replication.
	verHookMu sync.Mutex
	verHook   func(bucket, key string, rec VersionRecord)
	// metaDeleteHook propagates tombstones to the metadata replica set.
	metaDeleteHook func(name string)

	// metaResolver fetches metadata from the replica set when this node misses.
	metaResolver func(name string) (DistributedMeta, error)

	scrubDone    chan struct{}
	scrubWG      sync.WaitGroup
	scrubOnce    sync.Once
	scrubStarted atomic.Int32
	scrubLastRun atomic.Int64  // Unix nano timestamp of last scrub run
	scrubErrors  atomic.Uint64 // Cumulative scrub error count

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

	// R5 metrics: CAS GC counters read at /metrics scrape time
	// (phase 2, #933). Incremented inside the GC path only; scrape-only reads.
	gcRuns    atomic.Uint64
	gcEvicted atomic.Uint64

	// Phase 3: Metadata cache for distributed reads (TTL=60s)
	// Maps name -> cached metadata + expiry time
	metaCache    map[string]*cachedMeta
	metaCacheMu  sync.RWMutex
	metaCacheTTL time.Duration
	cacheMaxSize int
}

// cachedMeta holds a metadata entry with expiry time
type cachedMeta struct {
	meta   common.FileMetadata
	expiry time.Time
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

// metaBuckets is the fixed set of bbolt buckets created when a store opens.
var metaBuckets = [][]byte{
	bucketObjects,
	bucketNamespace,
	bucketPaths,
	bucketModTimes,
	bucketTombstones,
	bucketS3Meta,
	bucketQuarantine,
	bucketACL,
	bucketAuditLog,
	bucketTenantMeta,
	bucketVersioning,
	bucketObjectVersions,
	bucketLifecycle,
	bucketNotification,
	bucketObjectLockCfg,
	bucketAuditMeta,
	bucketLockState,
	bucketMetadataHints,
}

// initBuckets creates every metadata bucket if it does not already exist.
func initBuckets(tx *bbolt.Tx) error {
	for _, name := range metaBuckets {
		if _, err := tx.CreateBucketIfNotExists(name); err != nil {
			return err
		}
	}
	return nil
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
	if err := db.Update(initBuckets); err != nil {
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
		metaCache:    make(map[string]*cachedMeta),
		metaCacheTTL: 60 * time.Second,
		cacheMaxSize: 10000,
	}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

// Close shuts down the store, waiting for background GC, scrub, and rebuild
// goroutines to finish. It is idempotent: repeated calls are safe.
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
	return s.PutWithMetadata(name, hash, size, remotePath, content, nil, "", nil)
}

// PutWithMetadata is the internal implementation that accepts optional
// distributed metadata fields (VectorClock, ShardKey, MetadataReplicas).
// Used by the metadata RPC handler for quorum writes.
func (s *CASStore) PutWithMetadata(name string, hash string, size int64, remotePath string, content io.Reader, vectorClock []uint64, shardKey string, metadataReplicas []int32) (err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics in the storage backend.
	defer common.RecoverErr("CASStore.PutWithMetadata", &err)

	if common.HasPathTraversalChars(hash) {
		return fmt.Errorf("hash contains path traversal characters: %w", syscall.EINVAL)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Check if we already have the blob. Metadata says "exists" for a blob
	// whose bytes were shed; trusting it would ack a write without storing
	// anything, so ask the backend when it can tell us.
	exists, err := s.hasInternal(hash)
	if err != nil {
		return fmt.Errorf("failed to check blob existence: %w", err)
	}
	if exists {
		if p, ok := s.blobs.(blobPresence); ok {
			physical, perr := p.BlobExists(hash)
			if perr != nil {
				return fmt.Errorf("failed to check blob presence: %w", perr)
			}
			exists = physical
		}
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
	err = s.db.Update(func(tx *bbolt.Tx) error {
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
			// Increment our vector clock entry if provided
			if len(vectorClock) > 0 {
				// Ensure vector clock is at least as large as provided
				if len(meta.VectorClock) < len(vectorClock) {
					meta.VectorClock = make([]uint64, len(vectorClock))
				}
				for i := range vectorClock {
					if vectorClock[i] > meta.VectorClock[i] {
						meta.VectorClock[i] = vectorClock[i]
					}
				}
				// Increment our own entry (local node ID would be known in Phase 3)
				// For Phase 2, we just preserve the incoming clock
			}
		} else {
			meta = ObjectMeta{
				Size:             size,
				RefCount:         1,
				VectorClock:      vectorClock,
				ShardKey:         shardKey,
				MetadataReplicas: metadataReplicas,
			}
		}
		// Preserve distributed fields if not already set
		if meta.ShardKey == "" && shardKey != "" {
			meta.ShardKey = shardKey
		}
		if len(meta.MetadataReplicas) == 0 && len(metadataReplicas) > 0 {
			meta.MetadataReplicas = metadataReplicas
		}
		if err := obj.Put([]byte(hash), meta.encode()); err != nil {
			if errors.Is(err, syscall.ENOSPC) {
				return err
			}
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
			if errors.Is(err, syscall.ENOSPC) {
				return err
			}
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
				if errors.Is(err, syscall.ENOSPC) {
					return err
				}
				return fmt.Errorf("metadata error: %w", syscall.EIO)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Invalidate cache for this name since metadata was updated
	s.invalidateCache(name)
	return nil
}

// Get retrieves an object by its human-readable name.
func (s *CASStore) Get(name string) (rc io.ReadCloser, meta common.FileMetadata, err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics during metadata parsing.
	defer common.RecoverErr("CASStore.Get", &err)

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
// GetMeta returns one object's metadata, falling back to the distributed
// replica set when this node does not hold it (R6 v2 read path, #1142). The
// resolved record is applied locally, so the next read is a cache hit.
func (s *CASStore) GetMeta(name string) (common.FileMetadata, error) {
	meta, err := s.getMetaLocal(name)
	if err == nil || !errors.Is(err, syscall.ENOENT) || s.metadataResolver() == nil || s.isTombstoned(name) {
		return meta, err
	}
	if !s.loadMetaFromReplicas(name) {
		return meta, err
	}
	return s.getMetaLocal(name)
}

// getMetaLocal reads metadata from this node only.
func (s *CASStore) getMetaLocal(name string) (meta common.FileMetadata, err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics during metadata parsing.
	defer common.RecoverErr("CASStore.GetMeta", &err)

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readMetaLocalTx(name)
}

// readMetaLocalTx reads one record from the local database.
func (s *CASStore) readMetaLocalTx(name string) (common.FileMetadata, error) {
	hash, err := s.lookupHash(name)
	if err != nil {
		return common.FileMetadata{}, err
	}
	var size int64
	var remotePath string
	var modTime int64
	err = s.db.View(func(tx *bbolt.Tx) error {
		val := tx.Bucket(bucketObjects).Get([]byte(hash))
		if val == nil {
			return fmt.Errorf("metadata missing for hash %s: %w", hash, syscall.ENOENT)
		}
		decoded, derr := decodeObjectMeta(val)
		if derr != nil {
			return derr
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

// lookupHash resolves a live name to its content hash, honouring tombstones.
func (s *CASStore) lookupHash(name string) (string, error) {
	var hash string
	err := s.db.View(func(tx *bbolt.Tx) error {
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
		return "", err
	}
	return hash, nil
}

// DistributedMeta is the metadata a replica holds for one name, including the
// vector clock used by the distributed catalog to order concurrent writes.
type DistributedMeta struct {
	Hash             string
	Size             int64
	RemotePath       string
	VectorClock      []uint64
	ShardKey         string
	MetadataReplicas []int32
	ModTime          int64
}

// GetDistributedMeta returns the full local record for a name, including the
// vector clock (R6 v2 read path, #1142).
func (s *CASStore) GetDistributedMeta(name string) (dm DistributedMeta, err error) {
	defer common.RecoverErr("CASStore.GetDistributedMeta", &err)

	s.mu.RLock()
	defer s.mu.RUnlock()

	err = s.db.View(func(tx *bbolt.Tx) error {
		if tx.Bucket(bucketTombstones).Get([]byte(name)) != nil {
			return syscall.ENOENT
		}
		h := tx.Bucket(bucketNamespace).Get([]byte(name))
		if h == nil {
			return syscall.ENOENT
		}
		dm.Hash = string(h)
		val := tx.Bucket(bucketObjects).Get(h)
		if val == nil {
			return syscall.ENOENT
		}
		meta, derr := decodeObjectMeta(val)
		if derr != nil {
			return derr
		}
		dm.Size = meta.Size
		dm.VectorClock = meta.VectorClock
		dm.ShardKey = meta.ShardKey
		dm.MetadataReplicas = meta.MetadataReplicas
		if p := tx.Bucket(bucketPaths).Get([]byte(name)); p != nil {
			dm.RemotePath = string(p)
		}
		if mt := tx.Bucket(bucketModTimes).Get([]byte(name)); len(mt) >= 8 {
			dm.ModTime = int64(binary.BigEndian.Uint64(mt[:8]))
		}
		return nil
	})
	return dm, err
}

// SetMetadataResolver installs a fallback that fetches metadata from the
// distributed replica set when this node does not hold it (R6 v2 read path,
// #1142). nil disables the fallback.
func (s *CASStore) SetMetadataResolver(fn func(name string) (DistributedMeta, error)) {
	s.mu.Lock()
	s.metaResolver = fn
	s.mu.Unlock()
}

// isTombstoned reports whether this node recorded a deletion for the name.
// A tombstoned name must never be resolved from replicas: doing so would
// resurrect an object the owner deleted.
func (s *CASStore) isTombstoned(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	found := false
	_ = s.db.View(func(tx *bbolt.Tx) error {
		found = tx.Bucket(bucketTombstones).Get([]byte(name)) != nil
		return nil
	})
	return found
}

// metadataResolver returns the configured resolver (nil when disabled).
func (s *CASStore) metadataResolver() func(string) (DistributedMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metaResolver
}

// loadMetaFromReplicas applies the replica-set record for name locally.
func (s *CASStore) loadMetaFromReplicas(name string) bool {
	resolve := s.metadataResolver()
	if resolve == nil {
		return false
	}
	dm, err := resolve(name)
	if err != nil {
		if !errors.Is(err, syscall.ENOENT) {
			log.Printf("AUDIT: metadata resolve failed for %q: %v", name, err)
		}
		return false
	}
	if dm.Hash == "" {
		return false
	}
	// Metadata-only write: at-rest bytes are irrelevant here.
	if perr := s.PutWithMetadata(name, dm.Hash, dm.Size, dm.RemotePath, nil, dm.VectorClock, dm.ShardKey, dm.MetadataReplicas); perr != nil {
		log.Printf("AUDIT: failed to apply resolved metadata for %q: %v", name, perr)
		return false
	}
	return true
}

// getCachedMeta retrieves metadata from the local cache if present and not expired.
func (s *CASStore) getCachedMeta(name string) (common.FileMetadata, bool) {
	s.metaCacheMu.RLock()
	defer s.metaCacheMu.RUnlock()
	entry, ok := s.metaCache[name]
	if !ok {
		return common.FileMetadata{}, false
	}
	if time.Now().After(entry.expiry) {
		return common.FileMetadata{}, false
	}
	return entry.meta, true
}

// setCachedMeta stores metadata in the local cache with TTL.
// Performs LRU eviction if cache exceeds max size.
func (s *CASStore) setCachedMeta(name string, meta common.FileMetadata) {
	s.metaCacheMu.Lock()
	defer s.metaCacheMu.Unlock()

	if len(s.metaCache) >= s.cacheMaxSize {
		oldest := ""
		oldestTime := time.Time{}
		for k, v := range s.metaCache {
			if oldest == "" || v.expiry.Before(oldestTime) {
				oldest = k
				oldestTime = v.expiry
			}
		}
		if oldest != "" {
			delete(s.metaCache, oldest)
		}
	}

	s.metaCache[name] = &cachedMeta{
		meta:   meta,
		expiry: time.Now().Add(s.metaCacheTTL),
	}
}

// invalidateCache removes a name from the cache (e.g., on write/update/delete).
func (s *CASStore) invalidateCache(name string) {
	s.metaCacheMu.Lock()
	defer s.metaCacheMu.Unlock()
	delete(s.metaCache, name)
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
	defer common.RecoverErr("CASStore.Has", &err)

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
	defer common.RecoverErr("CASStore.GetHashForName", &err)

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

// Delete removes the object named name from the store. Panics are recovered
// and surfaced as EIO errors (zero-crash guarantee).
func (s *CASStore) Delete(name string) error {
	return s.delete(name, true)
}

// DeleteReplica applies a tombstone received from a peer without re-propagating
// it (R6 v2, #1142): re-broadcasting inside the replica handler would loop.
func (s *CASStore) DeleteReplica(name string) error {
	return s.delete(name, false)
}

// delete removes one object's metadata, propagating the tombstone when asked.
func (s *CASStore) delete(name string, propagate bool) (err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics.
	defer common.RecoverErr("CASStore.Delete", &err)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	var orphanedHash string
	if err = s.db.Update(func(tx *bbolt.Tx) error {
		h, txErr := s.deleteTx(tx, name, now)
		orphanedHash = h
		return txErr
	}); err != nil {
		return err
	}

	if orphanedHash != "" {
		s.evictOrphanedBlob(orphanedHash)
	}
	s.invalidateCache(name)
	// Propagate the tombstone to the metadata replica set (R6 v2, #1142).
	if propagate {
		s.notifyMetadataDeleted(name)
	}
	return nil
}

// deleteTx writes the tombstone, decrements the object's refcount and removes
// the name's index entries. It returns the hash whose last reference is gone.
func (s *CASStore) deleteTx(tx *bbolt.Tx, name string, now int64) (string, error) {
	ns := tx.Bucket(bucketNamespace)
	paths := tx.Bucket(bucketPaths)

	if err := putTombstone(tx.Bucket(bucketTombstones), name, now); err != nil {
		return "", err
	}

	orphaned, err := decrementRefCount(tx.Bucket(bucketObjects), ns.Get([]byte(name)), now)
	if err != nil {
		return "", err
	}
	if err := ns.Delete([]byte(name)); err != nil {
		return "", fmt.Errorf("namespace delete error: %w", err)
	}
	if err := paths.Delete([]byte(name)); err != nil {
		return "", fmt.Errorf("paths delete error: %w", err)
	}
	if err := tx.Bucket(bucketModTimes).Delete([]byte(name)); err != nil {
		return "", fmt.Errorf("modtime delete error: %w", err)
	}
	return orphaned, nil
}

// putTombstone records the deletion timestamp for a name.
func putTombstone(bucket *bbolt.Bucket, name string, now int64) error {
	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], uint64(now))
	if err := bucket.Put([]byte(name), tsBuf[:]); err != nil {
		return fmt.Errorf("metadata error: %w", syscall.EIO)
	}
	return nil
}

// decrementRefCount lowers the object's reference count and returns its hash
// when the count reaches zero.
func decrementRefCount(obj *bbolt.Bucket, hashBytes []byte, now int64) (string, error) {
	if hashBytes == nil {
		return "", nil
	}
	hash := string(hashBytes)
	val := obj.Get(hashBytes)
	if val == nil {
		return "", nil
	}
	meta, err := decodeObjectMeta(val)
	if err != nil {
		return "", fmt.Errorf("failed to decode metadata for hash %s: %w", hash, err)
	}
	meta.RefCount--
	if meta.RefCount <= 0 {
		meta.RefCount = 0
		meta.DeletedAt = now
	}
	if err := obj.Put(hashBytes, meta.encode()); err != nil {
		return "", fmt.Errorf("metadata error: %w", syscall.EIO)
	}
	if meta.DeletedAt == now {
		return hash, nil
	}
	return "", nil
}

// evictOrphanedBlob deletes blob bytes whose last reference is gone (CVE-006),
// outside the bbolt transaction to avoid blocking the DB during network I/O.
func (s *CASStore) evictOrphanedBlob(hash string) {
	if delErr := s.blobs.DeleteBlob(hash); delErr != nil {
		log.Printf("AUDIT: Failed to delete orphaned blob %s: %v", hash, delErr)
		return
	}
	// R5 metrics (#933): account for the immediately-evicted bytes.
	metaErr := s.db.Update(func(tx *bbolt.Tx) error {
		val := tx.Bucket(bucketObjects).Get([]byte(hash))
		if len(val) > 0 {
			if m, derr := decodeObjectMeta(val); derr == nil && m.Size > 0 {
				s.gcEvicted.Add(uint64(m.Size))
			}
		}
		return tx.Bucket(bucketObjects).Delete([]byte(hash))
	})
	if metaErr != nil {
		log.Printf("AUDIT: Failed to remove metadata for orphaned blob %s: %v", hash, metaErr)
	}
}

// List retrieves all file metadata entries in the store.
func (s *CASStore) List() (list []common.FileMetadata, err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics.
	defer common.RecoverErr("CASStore.List", &err)

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

// GetBlobPath resolves the on-disk path of the blob for the given name.
// Panics are recovered and surfaced as EIO errors (zero-crash guarantee).
func (s *CASStore) GetBlobPath(name string) (path string, err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics.
	defer common.RecoverErr("CASStore.GetBlobPath", &err)

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

// Stats returns R5 storage gauges computed at /metrics scrape time: the number
// of unique blobs in the objects bucket (non-tombstoned, refcounts>0) and the
// total logical bytes stored. This is a scrape-only read — never called on the
// request hot path (#933). Returns (blobCount, storedBytes, err).
func (s *CASStore) Stats() (blobCount int64, storedBytes int64, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	err = s.db.View(func(tx *bbolt.Tx) error {
		obj := tx.Bucket(bucketObjects)
		if obj == nil {
			return nil
		}
		return obj.ForEach(func(k, v []byte) error {
			meta, err := decodeObjectMeta(v)
			if err != nil {
				return err
			}
			if meta.DeletedAt == 0 && meta.RefCount > 0 {
				blobCount++
				storedBytes += meta.Size
			}
			return nil
		})
	})
	return blobCount, storedBytes, err
}

// Backup writes a consistent snapshot of the bbolt database to the provided writer.
// It uses a read-only transaction to ensure consistency without blocking writers.
// Uses bbolt's built-in WriteTo for a complete, consistent backup.
func (s *CASStore) Backup(w io.Writer) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Start a read-only transaction for consistent snapshot
	tx, err := s.db.Begin(false)
	if err != nil {
		return fmt.Errorf("failed to start read transaction for backup: %w", err)
	}
	defer tx.Rollback()

	// Use bbolt's built-in WriteTo for consistent backup
	if _, err = tx.WriteTo(w); err != nil {
		return fmt.Errorf("failed to write backup: %w", err)
	}

	return nil
}

// Restore restores a bbolt database from a backup file by copying the backup
// file directly to the output path. The backup file is a raw bbolt page stream
// produced by Backup(). force=true is required to overwrite an existing file.
func Restore(inputPath, outputPath string, force bool) error {
	// Open backup file
	f, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer f.Close()

	// Check if output file exists
	if _, err := os.Stat(outputPath); err == nil && !force {
		return fmt.Errorf("output file %s already exists (use --force to overwrite): %w", outputPath, syscall.EEXIST)
	}

	// Copy backup file to output path
	out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, f); err != nil {
		return fmt.Errorf("failed to copy backup to output: %w", err)
	}

	// Verify the restored database can be opened
	db, err := bbolt.Open(outputPath, 0600, nil)
	if err != nil {
		return fmt.Errorf("restored database failed to open: %w", err)
	}
	db.Close()

	log.Printf("AUDIT: Restored backup from %s to %s", inputPath, outputPath)
	return nil
}
func (s *CASStore) DataDir() string { return s.base }

// GetDiskUsage returns the used and free bytes of the filesystem containing the store.
// Returns an error if statfs fails.
func (s *CASStore) GetDiskUsage() (used, free uint64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.base, &stat); err != nil {
		return 0, 0, fmt.Errorf("statfs: %w", err)
	}
	// Total blocks * block size = total bytes
	total := stat.Blocks * uint64(stat.Bsize)
	// Free blocks for unprivileged users * block size = free bytes
	free = stat.Bavail * uint64(stat.Bsize)
	used = total - free
	return used, free, nil
}

// ScrubStatus returns the current status of the background integrity scrub.
// Returns (running, lastRunUnixNano, errorCount).
func (s *CASStore) ScrubStatus() (running bool, lastRun int64, errors uint64) {
	running = s.scrubStarted.Load() == 1 && s.scrubDone != nil
	lastRun = s.scrubLastRun.Load()
	errors = s.scrubErrors.Load()
	return running, lastRun, errors
}

// GCMetrics returns (gcRuns, gcEvictedBytes) collected by the CAS GC sweep.
func (s *CASStore) GCMetrics() (uint64, uint64) { return s.gcRuns.Load(), s.gcEvicted.Load() }

// IncGC records one completed GC run (call inside runGC at sweep end).
func (s *CASStore) IncGC() { s.gcRuns.Add(1) }

// AddGCEvicted records bytes physically removed by GC in this sweep.
func (s *CASStore) AddGCEvicted(n uint64) { s.gcEvicted.Add(n) }

// === R8 Tenant Metadata Methods ===

// PutTenantConfig stores a tenant configuration in the tenant_meta bucket.
func (s *CASStore) PutTenantConfig(tenant *common.TenantConfig) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketTenantMeta)
		if b == nil {
			return fmt.Errorf("tenant_meta bucket not found")
		}
		data, err := json.Marshal(tenant)
		if err != nil {
			return fmt.Errorf("failed to marshal tenant config: %w", err)
		}
		return b.Put([]byte(tenant.ID), data)
	})
}

// GetTenantConfig retrieves a tenant configuration by ID.
func (s *CASStore) GetTenantConfig(tenantID string) (*common.TenantConfig, error) {
	var tenant common.TenantConfig
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketTenantMeta)
		if b == nil {
			return fmt.Errorf("tenant_meta bucket not found")
		}
		data := b.Get([]byte(tenantID))
		if data == nil {
			return fmt.Errorf("tenant %q not found: %w", tenantID, syscall.ENOENT)
		}
		return json.Unmarshal(data, &tenant)
	})
	if err != nil {
		return nil, err
	}
	return &tenant, nil
}

// ListTenantConfigs returns all tenant configurations.
func (s *CASStore) ListTenantConfigs() ([]*common.TenantConfig, error) {
	var tenants []*common.TenantConfig
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketTenantMeta)
		if b == nil {
			return nil // bucket doesn't exist yet
		}
		return b.ForEach(func(k, v []byte) error {
			var tenant common.TenantConfig
			if err := json.Unmarshal(v, &tenant); err != nil {
				return err
			}
			tenants = append(tenants, &tenant)
			return nil
		})
	})
	return tenants, err
}

// DeleteTenantConfig removes a tenant configuration.
func (s *CASStore) DeleteTenantConfig(tenantID string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketTenantMeta)
		if b == nil {
			return nil
		}
		return b.Delete([]byte(tenantID))
	})
}

// === R8 Audit Log Methods ===

// auditPruneAnchorKey stores, in bucketAuditMeta, the EntryHash of the last
// audit entry removed by retention pruning. VerifyAuditLog starts its chain
// check from this hash so the retained window remains tamper-verifiable (#1307).
var auditPruneAnchorKey = []byte("pruned_through")

// PruneAuditLog deletes audit entries older than cutoff and returns how many
// were removed. The hash of the last deleted entry is recorded as the new chain
// anchor so VerifyAuditLog still validates the retained window.
//
// Retention is bounded by the operator's [audit] retention_days setting; it
// deliberately weakens tamper evidence for entries older than the window, which
// is the documented trade-off of a retention policy.
func (s *CASStore) PruneAuditLog(cutoff time.Time) (int, error) {
	if cutoff.IsZero() {
		return 0, nil
	}
	limit := uint64(cutoff.UnixNano())
	pruned := 0
	err := s.db.Update(func(tx *bbolt.Tx) error {
		n, perr := pruneAuditTx(tx, limit)
		pruned = n
		return perr
	})
	if err != nil {
		return 0, err
	}
	return pruned, nil
}

// pruneAuditTx deletes audit entries below limit and records the chain anchor.
// It is a free function so the exported method stays within the complexity
// budget (Rule 345).
func pruneAuditTx(tx *bbolt.Tx, limit uint64) (int, error) {
	b := tx.Bucket(bucketAuditLog)
	if b == nil {
		return 0, nil
	}
	pruned := 0
	anchor := ""
	c := b.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		if binary.BigEndian.Uint64(k) >= limit {
			break
		}
		var entry common.AuditLogEntry
		if err := json.Unmarshal(v, &entry); err != nil {
			return 0, fmt.Errorf("failed to unmarshal audit entry at %d: %v: %w", binary.BigEndian.Uint64(k), err, syscall.EIO)
		}
		anchor = entry.EntryHash
		if err := c.Delete(); err != nil {
			return 0, fmt.Errorf("failed to delete audit entry at %d: %v: %w", binary.BigEndian.Uint64(k), err, syscall.EIO)
		}
		pruned++
	}
	if pruned == 0 {
		return 0, nil
	}
	meta := tx.Bucket(bucketAuditMeta)
	if meta == nil {
		return 0, fmt.Errorf("audit_meta bucket not found: %w", syscall.EIO)
	}
	return pruned, meta.Put(auditPruneAnchorKey, []byte(anchor))
}

// WriteAuditLog appends an audit log entry with hash chaining for tamper evidence.
func (s *CASStore) WriteAuditLog(entry *common.AuditLogEntry) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketAuditLog)
		if b == nil {
			return fmt.Errorf("audit_log bucket not found")
		}

		// Get previous entry hash
		var prevHash string
		c := b.Cursor()
		if k, _ := c.Last(); k != nil {
			var lastEntry common.AuditLogEntry
			if v := b.Get(k); v != nil {
				if err := json.Unmarshal(v, &lastEntry); err == nil {
					prevHash = lastEntry.EntryHash
				}
			}
		}

		entry.Timestamp = time.Now().UnixNano()
		entry.PrevHash = prevHash

		// Compute entry hash (excluding EntryHash itself)
		entryData, _ := json.Marshal(struct {
			Timestamp int64  `json:"timestamp"`
			TenantID  string `json:"tenant_id"`
			Identity  string `json:"identity"`
			Operation string `json:"operation"`
			Resource  string `json:"resource"`
			Outcome   string `json:"outcome"`
			RequestID string `json:"request_id"`
			PrevHash  string `json:"prev_hash"`
		}{
			Timestamp: entry.Timestamp,
			TenantID:  entry.TenantID,
			Identity:  entry.Identity,
			Operation: entry.Operation,
			Resource:  entry.Resource,
			Outcome:   entry.Outcome,
			RequestID: entry.RequestID,
			PrevHash:  prevHash,
		})
		hash := sha256.Sum256(entryData)
		entry.EntryHash = hex.EncodeToString(hash[:])

		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("failed to marshal audit entry: %w", err)
		}

		// Use timestamp as key (nanoseconds since epoch, big-endian for sorting)
		var key [8]byte
		binary.BigEndian.PutUint64(key[:], uint64(entry.Timestamp))
		return b.Put(key[:], data)
	})
}

// VerifyAuditLog walks the audit log chain and verifies hash integrity.
// Returns error if any hash mismatch is found (tamper detection).
func (s *CASStore) VerifyAuditLog() error {
	return s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketAuditLog)
		if b == nil {
			return nil // No audit log yet
		}

		// A pruned log starts from the hash of the last deleted entry, so the
		// retained window stays verifiable (#1307).
		var prevHash string
		if meta := tx.Bucket(bucketAuditMeta); meta != nil {
			prevHash = string(meta.Get(auditPruneAnchorKey))
		}
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var entry common.AuditLogEntry
			if err := json.Unmarshal(v, &entry); err != nil {
				return fmt.Errorf("failed to unmarshal audit entry at %d: %w", binary.BigEndian.Uint64(k), err)
			}

			// Verify prev hash
			if entry.PrevHash != prevHash {
				return fmt.Errorf("audit log chain broken at timestamp %d: prev_hash mismatch (expected %q, got %q)", binary.BigEndian.Uint64(k), prevHash, entry.PrevHash)
			}

			// Verify entry hash
			entryData, _ := json.Marshal(struct {
				Timestamp int64  `json:"timestamp"`
				TenantID  string `json:"tenant_id"`
				Identity  string `json:"identity"`
				Operation string `json:"operation"`
				Resource  string `json:"resource"`
				Outcome   string `json:"outcome"`
				RequestID string `json:"request_id"`
				PrevHash  string `json:"prev_hash"`
			}{
				Timestamp: entry.Timestamp,
				TenantID:  entry.TenantID,
				Identity:  entry.Identity,
				Operation: entry.Operation,
				Resource:  entry.Resource,
				Outcome:   entry.Outcome,
				RequestID: entry.RequestID,
				PrevHash:  prevHash,
			})
			hash := sha256.Sum256(entryData)
			computedHash := hex.EncodeToString(hash[:])
			if entry.EntryHash != computedHash {
				return fmt.Errorf("audit log entry hash mismatch at timestamp %d: expected %q, got %q", binary.BigEndian.Uint64(k), computedHash, entry.EntryHash)
			}

			prevHash = entry.EntryHash
		}
		return nil
	})
}

// GetAuditLogEntries returns audit log entries for a time range (optional).
// auditTimeKey encodes a Unix-nano timestamp as an 8-byte big-endian key, or
// nil when t <= 0 (meaning unbounded).
func auditTimeKey(t int64) []byte {
	if t <= 0 {
		return nil
	}
	var k [8]byte
	binary.BigEndian.PutUint64(k[:], uint64(t))
	return k[:]
}

// collectAuditEntries walks the audit cursor from startKey (inclusive) to
// endKey (inclusive), decoding entries up to limit (0 = unbounded).
func collectAuditEntries(c *bbolt.Cursor, startKey, endKey []byte, limit int) []*common.AuditLogEntry {
	var entries []*common.AuditLogEntry
	for k, v := c.Seek(startKey); k != nil; k, v = c.Next() {
		if limit > 0 && len(entries) >= limit {
			break
		}
		if endKey != nil && binary.BigEndian.Uint64(k) > binary.BigEndian.Uint64(endKey) {
			break
		}
		var entry common.AuditLogEntry
		if err := json.Unmarshal(v, &entry); err != nil {
			continue
		}
		entries = append(entries, &entry)
	}
	return entries
}

// GetAuditLogEntries returns audit entries whose timestamp falls within
// [startTime, endTime], up to limit entries (0 = no limit).
func (s *CASStore) GetAuditLogEntries(startTime, endTime int64, limit int) ([]*common.AuditLogEntry, error) {
	var entries []*common.AuditLogEntry
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketAuditLog)
		if b == nil {
			return nil
		}
		entries = collectAuditEntries(b.Cursor(), auditTimeKey(startTime), auditTimeKey(endTime), limit)
		return nil
	})
	return entries, err
}

// ACL methods for R8 multi-tenancy authorization

// PutBucketACL stores a bucket ACL.
func (s *CASStore) PutBucketACL(acl *common.BucketACL) error {
	if acl == nil || acl.Bucket == "" {
		return fmt.Errorf("bucket ACL requires bucket name: %w", syscall.EINVAL)
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketACL)
		key := []byte(bucketResourcePrefix + acl.Bucket)
		data, err := json.Marshal(acl)
		if err != nil {
			return fmt.Errorf("failed to marshal ACL: %w", syscall.EIO)
		}
		return b.Put(key, data)
	})
}

// GetBucketACL retrieves a bucket ACL.
func (s *CASStore) GetBucketACL(bucket string) (*common.BucketACL, error) {
	if bucket == "" {
		return nil, fmt.Errorf("bucket name required: %w", syscall.EINVAL)
	}
	var acl common.BucketACL
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketACL)
		data := b.Get([]byte(bucketResourcePrefix + bucket))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, &acl)
	})
	return &acl, err
}

// PutObjectACL stores an object ACL.
func (s *CASStore) PutObjectACL(acl *common.ObjectACL) error {
	if acl == nil || acl.Key == "" {
		return fmt.Errorf("object ACL requires object key: %w", syscall.EINVAL)
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketACL)
		key := []byte(objectResourcePrefix + acl.Key)
		data, err := json.Marshal(acl)
		if err != nil {
			return fmt.Errorf("failed to marshal ACL: %w", syscall.EIO)
		}
		return b.Put(key, data)
	})
}

// GetObjectACL retrieves an object ACL.
func (s *CASStore) GetObjectACL(key string) (*common.ObjectACL, error) {
	if key == "" {
		return nil, fmt.Errorf("object key required: %w", syscall.EINVAL)
	}
	var acl common.ObjectACL
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketACL)
		data := b.Get([]byte(objectResourcePrefix + key))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, &acl)
	})
	return &acl, err
}

// checkACL evaluates an ACL entry against a tenant and permission.
func checkACL(acl *common.ACLEntry, tenantID string, perm common.Permission) bool {
	if acl == nil {
		return false
	}
	// Wildcard tenant matches all
	if acl.TenantID != "" && acl.TenantID != tenantID {
		return false
	}
	// Check permission match
	permMatch := false
	switch acl.Permission {
	case common.PermRead:
		permMatch = perm == common.PermRead || perm == common.PermAdmin
	case common.PermWrite:
		permMatch = perm == common.PermWrite || perm == common.PermAdmin
	case common.PermAdmin:
		permMatch = perm == common.PermAdmin
	case common.PermDelete:
		permMatch = perm == common.PermDelete || perm == common.PermAdmin
	}
	if !permMatch {
		return false
	}
	// Default effect is "allow" if not specified
	effect := acl.Effect
	if effect == "" {
		effect = "allow"
	}
	return effect == "allow"
}

// Resource prefixes for Authorize.
const (
	bucketResourcePrefix = "bucket/"
	objectResourcePrefix = "object/"
)

// parseACLResource splits an authorization resource into its bucket and object
// parts. Resource format: "bucket/<bucket>" or "object/<bucket>/<key>".
func parseACLResource(resource string) (bucket, object string, err error) {
	switch {
	case strings.HasPrefix(resource, bucketResourcePrefix):
		return strings.TrimPrefix(resource, bucketResourcePrefix), "", nil
	case strings.HasPrefix(resource, objectResourcePrefix):
		// ⚡ Bolt: Eliminate heap allocation by replacing strings.SplitN with strings.IndexByte
		rest := resource[len(objectResourcePrefix):]
		idx := strings.IndexByte(rest, '/')
		if idx == -1 {
			return "", "", fmt.Errorf("invalid object resource format: %w", syscall.EINVAL)
		}
		return rest[:idx], rest[idx+1:], nil
	default:
		return "", "", fmt.Errorf("invalid resource format: %w", syscall.EINVAL)
	}
}

// aclAllows reports whether any entry grants tenantID the permission.
func aclAllows(entries []common.ACLEntry, tenantID string, perm common.Permission) bool {
	for i := range entries {
		if checkACL(&entries[i], tenantID, perm) {
			return true
		}
	}
	return false
}

// Authorize checks if a tenant has permission to perform an operation on a resource.
// Returns nil if authorized, or an error with EACCES if denied.
// Resource format: "bucket/<bucket>" or "object/<bucket>/<key>"
func (s *CASStore) Authorize(tenantID string, perm common.Permission, resource string) error {
	if tenantID == "" {
		return fmt.Errorf("tenant ID required: %w", syscall.EINVAL)
	}
	// Admin tenant bypasses all ACL checks (convention: "admin" tenant ID)
	if tenantID == "admin" {
		return nil
	}

	bucket, object, err := parseACLResource(resource)
	if err != nil {
		return err
	}

	// Object ACL is the most specific rule; bucket ACL is the fallback.
	if object != "" {
		acl, aErr := s.GetObjectACL(bucket + "/" + object)
		if aErr != nil {
			return aErr
		}
		if acl != nil && aclAllows(acl.Entries, tenantID, perm) {
			return nil
		}
	}
	if bucket != "" {
		acl, aErr := s.GetBucketACL(bucket)
		if aErr != nil {
			return aErr
		}
		if acl != nil && aclAllows(acl.Entries, tenantID, perm) {
			return nil
		}
	}

	// No matching ACL = deny (default deny)
	return fmt.Errorf("access denied for tenant %s to %s: %w", tenantID, resource, syscall.EACCES)
}

// checkACL evaluates an ACL entry against a tenant and permission.

// GetTenantIDByAuthToken returns the tenant ID associated with the given auth token.
// Returns empty string and error if not found.
func (s *CASStore) GetTenantIDByAuthToken(authToken string) (string, error) {
	if authToken == "" {
		return "", fmt.Errorf("auth token required: %w", syscall.EINVAL)
	}

	var tenantID string
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketTenantMeta)
		if b == nil {
			return fmt.Errorf("tenant_meta bucket not found: %w", syscall.ENOENT)
		}

		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var cfg common.TenantConfig
			if err := json.Unmarshal(v, &cfg); err != nil {
				continue
			}
			// Use constant-time comparison to prevent timing attacks
			if subtle.ConstantTimeCompare([]byte(cfg.AuthToken), []byte(authToken)) == 1 {
				tenantID = string(k)
				return nil
			}
		}
		return fmt.Errorf("tenant not found for auth token: %w", syscall.ENOENT)
	})
	if err != nil {
		return "", err
	}
	return tenantID, nil
}
