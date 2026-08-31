# Inline Small Files (Data-on-MDT)

## Change ID
inline-small-files-v1

## Status
Ratched

## Summary
Store blobs smaller than a configurable threshold (default 4KB) inline in BoltDB `objects` bucket value, eliminating separate BlobStore access for small files. 6x faster reads for small files (most files in object storage).

## Motivation
Current MomoFS stores all blobs—even 1KB files—to separate BlobStore backend. Each read requires: BoltDB lookup (0.1ms) → BlobStore read (0.5ms) = 0.6ms. For small files (most object storage workloads), this is unnecessary overhead. Inline storage puts data in metadata, same pattern as Ceph's Data-on-MDT and Lustre's inline small files.

## Motivation Details
- Most files in object storage are < 4KB (config documents, metadata, thumbnails, JSON configs, etc.)
- 6x latency improvement for smallest files
- Reduces BlobStore load (network I/O, connection pool pressure)
- Zero data duplication: CAS semantics still apply (inline blobs still content-addressed)
- Backward compatible: existing code works; new opt-in per-bucket

## Owner
@alsotoes

## Dependencies
- BoltDB ObjectMeta extension with InlineData field
- PutBlob path modification (detect size ≤ threshold → inline)
- GetBlob path modification (check inline first, then BlobStore)
- ReadCache integration (small inline blobs hit cache naturally)
- Configuration: inline_threshold_mb (default 4KB = 0.004MB)

## Caveats
- Larger files (> threshold) stored normally in BlobStore (no change)
- Inline data limited by BoltDB value size (currently ~16MB per value, more than enough)
- Node upgrade required: old daemons cannot read inline metadata from new daemons
- Migration path: re-Put existing files to move from BlobStore → inline

## Technical Details

### ObjectMeta Extension
```go
type ObjectMeta struct {
    Size       int64
    RefCount   int64
    DeletedAt  int64
    Checksum   uint32      // CRC32C of blob content
    InlineData []byte      // present if Size <= inlineThreshold (e.g., 4KB)
    Replicas   []int       // data node IDs from CRUSH (empty for inline-only)
    VectorClock []uint64   // for conflict resolution (empty for inline-only typically)
    ShardKey   string      // consistent hash ring shard key
}
```

### Inline Detection Logic
```go
const inlineThreshold = 4 * 1024 // 4KB

func isInline(size int64) bool {
    return size <= inlineThreshold
}
```

### PutBlob Path
```
1. Client → Node C: PUT /smallfile.txt (1KB content)
2. Node C: hash = sha256(content) = "abc123..."
3. Node C: size = 1024
4. Node C: isInline(size) → true
5. Node C: encode ObjectMeta with InlineData = content bytes
6. Node C: write to BoltDB namespace → hash, objects[hash] → ObjectMeta{InlineData:[0x01,0x02,...]}
7. Node C: return "200 OK" to client
```

### GetBlob Path
```
1. Client → Node X: GET /smallfile.txt
2. Node X: resolve metadata → {hash, size: 1024, inline: true, inlineData present}
3. Node X: BoltDB lookup objects[hash] → ObjectMeta{InlineData:[...]}
4. Node X: return inlineData directly → no BlobStore access
5. Latency: BoltDB lookup only → ~0.1ms (vs. 0.6ms previous)
```

### Read Path Integration
```
func (s *Store) Get(name string) (io.ReadCloser, FileMetadata, error) {
    // 1. Resolve metadata (existing distributed or local)
    meta, err := resolveMetadata(name)
    if err != nil { return nil, FileMetadata{}, err }
    
    // 2. Check tombstone
    if !meta.DeletedAt.IsZero() { return nil, FileMetadata{}, ENOENT }
    
    // 3. Check blob cache
    if cached, ok := cache.GetBlob(meta.Hash); ok {
        return cached, meta, nil
    }
    
    // 4. Check inline first (new)
    if isInline(meta.Size) {
        // Get from BoltDB directly
        rc, err := s.local.GetInline(meta.Hash)
        if err == nil { 
            cache.PutBlob(meta.Hash, io.ReadAll(rc)) // cache for next time
            return rc, meta, nil 
        }
    }
    
    // 5. Fall back to BlobStore (existing path)
    return s.blobs.GetBlob(meta.Hash)
}
```

### Configuration

```toml
[momofs]
inline_threshold_mb = 0.004  # 4KB default — inline if size <= 4KB
```

### Migration Strategy

**Phase 1**: New clusters default to inline. Existing clusters can opt-in.

**Phase 2**: Re-Put migration path.
```
# Migration script (run once per bucket):
for each file in bucket:
    old_meta = GetMetadata(name)
    if old_meta.size <= 4KB and old_meta stored in BlobStore:
        DeleteObject(name)  # removes from BlobStore, keeps tombstone briefly
        PutObject(name, reader)  # re-puts → now inline in BoltDB
# Or bulk: export all blobs, delete bucket, re-import with inline enabled
```

### Backward Compatibility

**When `momofs.enabled = false`** (local-only mode): No change, all blobs go to BlobStore as before.

**When `inline_threshold_mb = 0`** (disabled): All blobs stored in BlobStore, no inline.

**Upgrade path**: 
1. New cluster: inline enabled by default (4KB threshold)
2. Existing cluster: set `inline_threshold_mb = 0` to disable, gradual opt-in
3. Migration: `momo rebalance --migrate-inline` (future Phase 2 feature)

### Performance Impact

| File Size | Before (BoltDB → BlobStore) | After (BoltDB inline) | Improvement |
|-----------|----------------------------|----------------------|-------------|
| 1KB | 0.1ms + 0.5ms = 0.6ms | 0.1ms | 6× faster |
| 4KB | 0.1ms + 0.5ms = 0.6ms | 0.1ms | 6× faster |
| 10KB | 0.1ms + 0.5ms = 0.6ms | 0.1ms (inline) or BlobStore | Same |
| 1MB | 0.1ms + 0.5ms = 0.6ms | BlobStore only (not inline) | No change |
| 100MB | BlobStore only | BlobStore only | No change |

**Aggregate**: If 70% of files are < 4KB (typical object storage), overall read latency improves ~2×.

**Blobsotre load**: 70% reduction in small-blob network I/O + disk reads.

### Testing Strategy

1. **Unit tests**: Put/Get small files (1KB, 4KB), verify inline storage in BoltDB
2. **Comparison tests**: Same files before/after inline, measure latency
3. **Large files**: Verify >4KB still go to BlobStore unchanged
4. **Concurrent**: Simultaneous Put/Get inline files, no corruption
5. **Migration**: Re-Put existing files, verify no data loss

### Acceptance Criteria
- [ ] 1KB file Put → stored inline in BoltDB (verify ObjectMeta encoding)
- [ ] 1KB file Get → returned from BoltDB, no BlobStore RPC
- [ ] 5KB file → stored in BlobStore (above threshold)
- [ ] 4KB file → boundary case: inline (configurable)
- [ ] ListObjectsV2 → works identically (inline transparent to client)
- [ ] S3 API → PUT/GET small files works (internal inline invisible)
- [ ] Cache → small inline blobs cached after first read
- [ ] Backward compat: `inline_threshold_mb = 0` → all BlobStore

### Open Questions / Open Issues
1. Inline data size limit: BoltDB value max ~16MB, is 4KB sufficient? (Yes, covers ~99% of object storage files)
2. What happens if InlineData exceeds BoltDB value size? (Fallback to BlobStore — error or auto-promote?)
3. Inline + versioning: if versioning enabled, does old version stay inline or go to BlobStore?
4. Inline + checksum: CRC32C computed on inline bytes or separate? (computed on inline bytes)
5. GC interaction: inline blobs GC same as others (refcount → 0 → delete from BoltDB)

### Linked GitHub Issues
- Issue #XXX: Inline small files performance improvement
- Issue #YYY: BoltDB ObjectMeta extension for inline data
- Issue #ZZW: 4KB inline threshold — is it right size?