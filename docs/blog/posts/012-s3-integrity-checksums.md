---
title: 'S3 Integrity Checksums: x-amz-checksum-*'
date: 2026-08-24 16:36:34+00:00
draft: false
post_type: architecture
tags:
- go
- s3
- checksum
- integrity
- sentinel
categories:
- s3
summary: 'x-amz-checksum-CRC32/SHA256 echo on PUT; integrity surfaced to clients so dirty bytes never masquerade as clean. Teaches: client-verifiable integrity, Bolt zero-allocation on read path, Sentinel honesty guarantee.'
artifacts:
- type: pr
  id: '902'
- type: spec
  path: openspec/changes/s3-integrity-checksums
related:
- 007-at-rest-integrity-and-gc
- 008-s3-gateway-core
- 009-s3-multipart-and-breadth
- 031-core-integrity-verification
- 045-bolt-lastmodified-header
difficulty: "intermediate"
pattern: "client-verifiable-integrity"
teaches:
  - "S3 checksum headers (x-amz-checksum-*)"
  - "client-verifiable integrity contract"
  - "Bolt: zero-allocation checksum on read path"
  - "Sentinel: verify-on-read honesty"
---
S3 Integrity Checksums: x-amz-checksum-*

## The Real Problem

We had integrity *inside* the store (verify-on-read, CAS hashing). But clients had to **trust us**. 

A security review asked: "If your server is compromised, how does the client know?" The answer was: they didn't. The checksum was an internal implementation detail, not a client contract.

We needed to surface integrity to the wire protocol — make it **client-verifiable**.

## Why Obvious Solutions Failed

**Custom header with proprietary format**
```go
// REJECTED: not interoperable, SDKs won't understand it
// "X-Momo-Checksum: sha256:abc123..." — only our SDK works
w.Header().Set("X-Momo-Checksum", "sha256:"+hash)
```

**ETag with checksum**
```go
// REJECTED: ETag is opaque, semantics unclear (strong vs weak)
// S3 uses ETag for conditional requests, not integrity
w.Header().Set("ETag", `"`+hash+`"`)
```

**Separate verification API**
```go
// REJECTED: extra round-trip, clients won't call it
// GET /verify/{key} → {hash} — adds latency, breaks streaming
```

## The Solution — S3 Native Checksum Headers

![S3 integrity checksums flow](/diagrams/13-s3-checksums.svg)

S3 defines `x-amz-checksum-*` headers for exactly this. We implement them natively:

```go
// PUT: client sends or requests checksum
func (h *S3Handler) PutObject(w http.ResponseWriter, r *http.Request) {
    // 1. Client may provide checksum (x-amz-checksum-sha256)
    clientHash := r.Header.Get("x-amz-checksum-sha256")
    
    // 2. Stream data through hasher (zero-copy)
    hasher := sha256.New()
    tee := io.TeeReader(r.Body, hasher)
    
    // 3. Write to CAS (hash = key)
    blobID, err := h.store.Write(tee)
    if err != nil {
        h.error(w, err)
        return
    }
    
    // 4. If client provided checksum, VERIFY
    if clientHash != "" && clientHash != hex.EncodeToString(hasher.Sum(nil)) {
        h.error(w, ErrChecksumMismatch)
        return
    }
    
    // 5. Echo checksum back — client verifies
    w.Header().Set("x-amz-checksum-sha256", hex.EncodeToString(hasher.Sum(nil)))
    w.WriteHeader(http.StatusOK)
}

// GET: return stored checksum (cached with metadata)
func (h *S3Handler) GetObject(w http.ResponseWriter, r *http.Request) {
    meta, err := h.store.GetMetadata(key)
    if err != nil {
        h.error(w, err)
        return
    }
    
    // Checksum cached with metadata — zero allocation on read path
    w.Header().Set("x-amz-checksum-sha256", meta.ChecksumSHA256)
    // ... stream body ...
}
```

## Principle Callout

> **Pattern: Client-Verifiable Integrity via Native Protocol Headers**
> Use the protocol's native checksum mechanism (S3: `x-amz-checksum-*`). Compute once on ingest, cache with metadata, echo on read. Zero trust — client verifies.
> 
> **Applies when**: Protocol has native checksum support (S3, HTTP digest, TLS)
> **Doesn't apply**: Protocols without checksum semantics (raw TCP, custom protocols)

```
┌─────────────────────────────────────────────────────────────────┐
│                    INTEGRITY FLOW                               │
├─────────────────────────────────────────────────────────────────┤
│  PUT                                    GET                    │
│  ────                                   ────                   │
│  Client sends                          Server returns          │
│  x-amz-checksum-sha256    ──────►       x-amz-checksum-sha256  │
│       │                                       │                │
│       ▼                                       ▼                │
│  Server hashes          ◄──────        Client compares        │
│  on ingest (CAS)                          with local hash     │
│       │                                       │                │
│       ▼                                       ▼                │
│  If mismatch: 400                    Mismatch = corruption    │
│  Bad Request                              detection            │
└─────────────────────────────────────────────────────────────────┘
```

## ⚡ Bolt + 🛡 Sentinel Overlap

### Sentinel: Honesty Guarantee

The echoed checksum is only as honest as the verify-on-read machinery:

```go
// Verify-on-read: EVERY read hashes and compares
func (s *Store) Get(blobID BlobID) (io.Reader, error) {
    data, err := s.backend.Get(blobID)
    if err != nil {
        return nil, err
    }
    
    // Verify-on-read: hash matches blob ID (which IS the hash)
    h := sha256.New()
    if _, err := io.Copy(h, data); err != nil {
        return nil, err
    }
    if BlobID(h.Sum(nil)) != blobID {
        return nil, ErrCorruptionDetected  // 🛡 Sentinel: fail loud
    }
    
    // Checksum cached with metadata — already verified
    return data, nil
}
```

The `x-amz-checksum-sha256` header returns `meta.ChecksumSHA256` which **came from the verified write path**. If bits flipped on disk, verify-on-read catches it before the checksum is ever echoed.

### Bolt: Zero-Allocation Checksum on Read Path

```go
// Checksum cached with metadata — no re-hashing on GET
type ObjectMetadata struct {
    Size           int64
    ChecksumSHA256 string  // pre-computed, cached
    // ...
}

// GET handler — zero allocation for checksum
func (h *S3Handler) GetObject(w http.ResponseWriter, r *http.Request) {
    meta := h.store.GetMetadata(key)  // includes cached checksum
    w.Header().Set("x-amz-checksum-sha256", meta.ChecksumSHA256)
    // ... stream from verified backend ...
}
```

See [024](024-bolt-performance-engineering.md) for the zero-escape pattern; [007](007-at-rest-integrity-and-gc.md) for verify-on-read.

## Verification

| Test | Result |
|------|--------|
| Client provides correct checksum | 200 OK, checksum echoed |
| Client provides wrong checksum | 400 Bad Request |
| Server corruption (bit-flip) | Verify-on-read catches, 500 before echo |
| AWS SDK compatibility | `aws s3 cp` validates automatically |
| Allocation on GET | 0 B/op for checksum (cached) |

## Failure Modes

| Risk | Mitigation |
|------|------------|
| Client doesn't verify | SDK default behavior; document explicitly |
| Checksum header stripped by proxy | Use TLS (enforced per [011](011-s3-https-tls-enforcement.md)) |
| Multipart upload checksum | Aggregated per [009](009-s3-multipart-and-breadth.md) |

## When NOT to Use This

- **Internal-only APIs** where trust boundary is the network (use mTLS instead)
- **Protocols without checksum semantics** — don't invent custom headers
- **When compute cost > value** — CRC32C for large objects, SHA256 for small

See [docs/STANDARDS.md](../../STANDARDS.md) (`docs/STANDARDS.md`) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

Integrity core: [007](007-at-rest-integrity-and-gc.md). Gateway: [008](008-s3-gateway-core.md). Performance: [024](024-bolt-performance-engineering.md).