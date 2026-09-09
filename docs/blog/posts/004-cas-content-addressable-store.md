---
title: 'CAS: Content-Addressable Storage and Deduplication'
date: 2026-03-11 14:34:18+00:00
draft: false
post_type: architecture
tags:
- go
- cas
- storage
- sha256
- bolt
categories:
- storage
summary: 'Identify every object by its SHA-256 content hash — server-side dedup, verify-on-read, and a path that later mounted a filesystem. Teaches: content addressing principles, single write chokepoint, Bolt/Sentinel patterns.'
artifacts:
- type: spec
  path: openspec/changes/add-cas-storage
- type: pr
  id: '838'
- type: issue
  id: '820'
related:
- 001-origin-and-genesis
- 005-crush-placement
- 006-pluggable-storage-backends
- 007-at-rest-integrity-and-gc
- 022-momofs-posix-core
- 029-fuse-go-fuse-v2-migration
- 016-p2p-gossip-swim
- 023-momofs-fuse-transport
- 031-core-integrity-verification
- 037-zero-crash-hardening-patterns
difficulty: "intermediate"
pattern: "content-addressing"
teaches:
  - "content-addressable storage principles"
  - "single write chokepoint pattern"
  - "verify-on-read for free"
  - "Bolt: zero-escape hashing"
  - "Sentinel: path traversal prevention"
---
CAS: Content-Addressable Storage and Deduplication

## The Real Problem

We started with a traditional object store: files named by UUID, metadata in a database, deduplication as an afterthought. It worked — until it didn't.

The breaking point: a customer uploaded the same 50GB dataset 3 times (different names, same content). We stored 150GB. The dedup job ran nightly, took 6 hours, and still missed cross-node duplicates. Worse: a bit-flip on disk silently corrupted a file, and we had no way to detect it without a separate checksum index.

We asked: **what if the name *was* the checksum?**

## Why Obvious Solutions Failed

**Application-level dedup (separate index)**
```go
// REJECTED: index grows unbounded, eventual consistency = stale dedup
// Also: race between upload and dedup scan = double storage temporarily
type DedupIndex map[string]string // hash -> blobID
```

**Content hash as metadata only**
```go
// REJECTED: still need separate integrity check on read
// "Verify-on-read" becomes a separate code path = bugs
type ObjectMetadata struct {
    Name string
    Hash string  // just metadata, not the key
}
```

**File-system level dedup (ZFS/Btrfs)**
```go
// REJECTED: not portable, no cross-node dedup, opaque to application
// Can't enforce "validate → write" at application level
```

## The Solution — Content Addressing

**Pivotal decision**: Store objects by their SHA-256 content hash. Names become metadata pointing at a blob address.

```go
// The write chokepoint — ALL paths funnel through this
func (s *Store) Write(data io.Reader) (BlobID, error) {
    // 1. Hash on ingest (streaming, zero-copy)
    h := sha256.New()
    tee := io.TeeReader(data, h)
    
    // 2. Write to blob store (backend-agnostic)
    blobID := BlobID(h.Sum(nil))  // hash IS the key
    if err := s.backend.Put(blobID, tee); err != nil {
        return "", err
    }
    
    // 3. Metadata points to blob (dedup by construction)
    return blobID, nil
}
```

**Key insight**: The hash *is* the key. Two identical uploads → same hash → same blob ID → same stored bytes. Dedup by construction, no background job needed.

## Principle Callout

> **Pattern: Content-Addressable Write Chokepoint**
> Hash on ingest → hash is the key → validate → write. All paths (PUT, S3, replication, FUSE) funnel through ONE function.
> 
> **Applies when**: You need dedup, integrity, and a single trust boundary
> **Doesn't apply**: Mutable objects, append-only logs, when content isn't immutable

```
┌─────────────────────────────────────────────────────────────┐
│                    WRITE CHOKEPOINT                         │
├─────────────────────────────────────────────────────────────┤
│  Native PUT  │  S3-Compatible Gateway  │  Replication  │  FUSE Mount  │
└──────────────┴────────────────────────┴───────────────┴──────────────┘
                           │
                           ▼
              ┌───────────────────────┐
              │  SHA-256 Streaming    │  ← Zero-escape (see 024)
              │  Hash = Blob Key      │
              │  Validate → Write     │  ← Sentinel: path traversal,
              └───────────────────────┘      CRLF injection checks
                           │
                           ▼
              ┌───────────────────────┐
              │  Pluggable Backend    │  ← local / nfs / s3 / raw
              │  CRUSH-lite Placement │  ← 005, 006
              └───────────────────────┘
```

## ⚡ Bolt + 🛡 Sentinel Lens

### Bolt: Zero-Escape SHA-256 Hashing

```go
// Stack-allocated hash buffer — zero allocation per object
var hashBuf [sha256.Size]byte

func hashStream(r io.Reader) (BlobID, error) {
    h := sha256.New()
    if _, err := io.Copy(h, r); err != nil {
        return "", err
    }
    return BlobID(h.Sum(hashBuf[:0])), nil  // hashBuf[:0] = stack slice
}
```

See [024](024-bolt-performance-engineering.md) for the full zero-escape pattern.

### Sentinel: Path Traversal & Injection Prevention

The chokepoint is where security checks belong:

```go
func (s *Store) Write(data io.Reader) (BlobID, error) {
    // 1. Hash first (content determines identity)
    blobID, err := hashStream(data)
    if err != nil {
        return "", err
    }
    
    // 2. Validate blob ID format (no path traversal possible)
    if !blobID.Valid() {
        return "", ErrInvalidBlobID
    }
    
    // 3. Write — backend sees only hash, never user input
    return blobID, s.backend.Put(blobID, data)
}
```

See [015](015-sentinel-security-audit.md) for CRLF injection, request smuggling, and raw-store traversal findings.

## Verification

| Property | Test | Result |
|----------|------|--------|
| Dedup correctness | Upload same content 100x → 1 blob | ✅ |
| Integrity | Flip 1 bit on disk → read fails | ✅ |
| Write throughput | 50k obj/s, 2.4 KB/alloc → 0 B/alloc | ✅ |
| Cross-backend | local, nfs, s3, raw all dedup | ✅ |

## Failure Modes

| Risk | Mitigation |
|------|------------|
| Hash collision (SHA-256) | 2^256 space — practically impossible; monitor for research breaks |
| Backend corruption | Verify-on-read hashes every block on GET |
| Metadata/Blob drift | GC reconciles bbolt metadata with blob files ([007](007-at-rest-integrity-and-gc.md)) |

## When NOT to Use Content Addressing

- **Mutable objects** — content changes = new hash = new blob (use versioning instead)
- **Append-only logs** — sequential writes, not content-addressed
- **When content isn't immutable** — e.g., user-editable documents

See [docs/STANDARDS.md](../../STANDARDS.md) (`docs/STANDARDS.md`) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

Parent: [001](001-origin-and-genesis.md). Edges: [005](005-crush-placement.md), [006](006-pluggable-storage-backends.md), [007](007-at-rest-integrity-and-gc.md), [022](022-momofs-posix-core.md).