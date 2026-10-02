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
Our first object store named files by random UUID, kept metadata in a database,
and treated deduplication as a cleanup job bolted on afterward. That design held
up until it didn't, and the failure taught us the most important lesson in the
storage core: the name of an object should be derived from its content, not
assigned arbitrarily.

## The Real Problem

A customer uploaded the same 50 GB dataset three times under different names. We
stored 150 GB. The nightly dedup job took six hours and still missed duplicates
that landed on different nodes. Worse, when a bit flipped on disk and silently
corrupted a file, we had no way to detect it without a separate checksum index
that could itself drift out of sync.

We asked: **what if the name *was* the checksum?**

That idea is **content-addressable storage** (CAS): instead of inventing an
identifier, hash the object's bytes and use the hash as its address. SHA-256 is
the hash momo uses. Two identical byte streams necessarily produce the same
hash, so identical uploads collide *on purpose* and deduplicate for free.

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

Each of these keeps the identifier and the integrity check as two separate
things that can disagree. Content addressing fuses them.

## The Solution — Content Addressing

**Pivotal decision:** store objects by their SHA-256 content hash. Names become
metadata pointing at a blob address.

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

**Key insight:** the hash *is* the key. Two identical uploads yield the same
hash, therefore the same blob ID, therefore the same stored bytes. Dedup happens
by construction — no background job, no window where duplicates coexist.

## Principle Callout

> **Pattern: Content-Addressable Write Chokepoint**
> Hash on ingest → hash is the key → validate → write. Every write path (native
> PUT, S3 gateway, replication, FUSE mount) funnels through ONE function.
> 
> **Applies when**: you need dedup, integrity, and a single trust boundary.
> **Doesn't apply**: mutable objects, append-only logs, or any case where the
> content is not immutable.

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

`h.Sum` normally allocates a fresh slice for the digest. Passing a stack-array
slice with length zero lets the hash write into memory that never escapes to the
heap — zero bytes allocated per object. See
[024](024-bolt-performance-engineering.md) for the full zero-escape pattern.

### Sentinel: Path Traversal & Injection Prevention

The chokepoint is the natural place for security checks, because every write
passes through it:

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

Because the backend is handed a validated hash and never a user-supplied name,
there is no filename for an attacker to traverse with `../` or to smuggle CRLF
into. See [015](015-sentinel-security-audit.md) for the CRLF injection, request
smuggling, and raw-store traversal findings that hardened this path.

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
| Metadata/Blob drift | GC reconciles metadata with blob files ([007](007-at-rest-integrity-and-gc.md)) |

## When NOT to Use Content Addressing

- **Mutable objects** — a content change means a new hash and a new blob; use
  versioning instead.
- **Append-only logs** — these are written sequentially, not looked up by
  content.
- **When content isn't immutable** — e.g. user-editable documents that change
  in place.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Spec: `openspec/changes/add-cas-storage`.
- Pull request #838; tracking issue #820.
- Parent: [001: Origin and Genesis](001-origin-and-genesis.md). Edges:
  [005](005-crush-placement.md), [006](006-pluggable-storage-backends.md),
  [007](007-at-rest-integrity-and-gc.md), [022](022-momofs-posix-core.md).
- Further reading: [015](015-sentinel-security-audit.md),
  [024](024-bolt-performance-engineering.md).
