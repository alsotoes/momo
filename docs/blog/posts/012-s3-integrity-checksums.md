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
A checksum is a short digest computed from a block of data: change one byte and
the digest changes, so anyone can recompute it and compare. The S3 protocol
carries checksums as `x-amz-checksum-*` response headers. This post is about
moving integrity from something the server does privately to something the client
can verify for itself.

## The Real Problem

We already had strong integrity *inside* the store. Objects are
content-addressed — an object's identity is the hash of its bytes — and every
read re-hashes the data and compares it against that identity before returning
it. If bits rotted on disk, the store noticed and failed loudly.

But clients could not see any of that. They had to **trust us**. A security
reviewer asked the uncomfortable question: "If your server is compromised, how
does the client know the bytes it received are the bytes it asked for?" The
honest answer was: it doesn't. The checksum was an internal implementation
detail, not a client contract.

That is a real gap. Integrity only the server can check is integrity the client
must take on faith. We needed to surface it on the wire so the client could
verify independently.

## Why the Obvious Solutions Failed

**A custom header with a proprietary format.** We could invent
`X-Momo-Checksum: sha256:...`, but no SDK understands it. Only our own client
would verify it, which defeats the point of an S3-compatible gateway. Rejected:
not interoperable.

**Reusing the ETag.** The ETag is an opaque identifier S3 uses for conditional
requests and caching; its meaning is not a promise about content integrity.
Overloading it would confuse clients that already depend on it. Rejected: wrong
semantics.

**A separate verification API.** We could expose an endpoint that returns the
hash for a key. But that adds a round-trip, and clients will not call it — the
point is to verify the bytes already received, not to ask a second request about
them. Rejected: extra latency, breaks streaming.

## The Solution: Native S3 Checksum Headers

{{< diagram src="/diagrams/13-s3-checksums.svg" alt="S3 integrity checksums flow" caption="S3 integrity checksums flow" >}}

S3 already defines the right mechanism: `x-amz-checksum-*` headers. We implement
them natively. The lifecycle is:

1. **On upload**, the server hashes the incoming bytes as they stream to the
   store — the hasher runs alongside the write, so the whole object never sits in
   memory.
2. If the client supplied a checksum, the server **compares** it against the
   computed digest and rejects a mismatch instead of storing bad data.
3. The computed checksum is **cached with the object's metadata**, so it never
   needs to be recomputed.
4. **On download**, the server echoes the cached checksum in the response
   headers, and the client compares it against its own hash of the body it
   received.

Here is the shape of that path, illustratively:

```go
// Illustrative pseudo-code — the real handler is wired through the store.
// On PUT, hash while streaming so the object never sits in memory.
hasher := sha256.New()
tee := io.TeeReader(r.Body, hasher)
blobID, err := store.Write(tee) // content-addressed: blobID is the hash

// If the client declared a checksum, reject a mismatch now.
if declared != "" && declared != hex(hasher.Sum(nil)) {
    return checksumMismatch
}
meta.ChecksumSHA256 = hex(hasher.Sum(nil)) // cached with the object

// On GET, the cached digest is echoed — no re-hashing on the read path.
w.Header().Set("x-amz-checksum-sha256", meta.ChecksumSHA256)
```

Because the checksum is captured at write time and stored beside the object, the
read path stays allocation-free: echoing a cached string costs nothing.

## Principle Callout

> **Pattern: Client-Verifiable Integrity via Native Protocol Headers**
> Use the protocol's own integrity mechanism (S3: `x-amz-checksum-*`). Compute
> once on ingest, cache with metadata, echo on read. The client verifies; the
> server does not ask to be trusted.
>
> **Applies when**: the protocol has native checksum support (S3, HTTP digests,
> TLS records).
> **Doesn't apply**: protocols without checksum semantics — do not invent a
> custom header and call it integrity.

## ⚡ Bolt & 🛡 Sentinel

🛡 **Sentinel: the honesty guarantee.** An echoed checksum is only as honest as
the machinery behind it. Verify-on-read means every read re-hashes the data and
compares it against the object's content-addressed identity. If bits flipped on
disk, the read fails loudly *before* the checksum is ever echoed. The header is
backed by a check, not by a stored claim.

⚡ **Bolt: zero allocation on the read path.** Because the checksum is cached with
metadata, a `GET` sets the header from an existing string — no re-hashing, no
allocation. The ingest path hashes once, streaming, so it never buffers the whole
object.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## How We Verified

| Test | Expected result |
|------|-----------------|
| Client provides the correct checksum | `200 OK`, checksum echoed |
| Client provides the wrong checksum | `400 Bad Request`, nothing stored |
| Server corruption (a flipped bit) | Verify-on-read fails before the echo |
| AWS SDK compatibility | `aws s3 cp` validates automatically |
| Allocation on `GET` | Zero allocation for the checksum (cached) |

## Failure Modes

| Risk | Mitigation |
|------|------------|
| Client does not verify | SDK default behaviour; documented explicitly |
| A proxy strips the checksum header | TLS is enforced, so the header cannot be rewritten in transit ([011](011-s3-https-tls-enforcement.md)) |
| Multipart upload checksum | Parts are aggregated into one checksum at completion ([009](009-s3-multipart-and-breadth.md)) |

## When NOT to Use This

- **Internal-only APIs** where the network itself is the trust boundary — mutual
  TLS is the stronger tool there.
- **Protocols without checksum semantics** — do not invent a custom header and
  pretend it is a standard.
- **When compute cost outweighs the value** — a fast checksum such as CRC32 is
  enough for large objects, while a cryptographic hash such as SHA-256 is worth
  it for small, security-sensitive ones.

## References / Dig deeper

- Integrity core: [007](007-at-rest-integrity-and-gc.md). Gateway:
  [008](008-s3-gateway-core.md). Multipart:
  [009](009-s3-multipart-and-breadth.md). Performance:
  [024](024-bolt-performance-engineering.md). Transport security:
  [011](011-s3-https-tls-enforcement.md).
- Core integrity verification: [031](031-core-integrity-verification.md).
- The `Last-Modified` header optimization: [045](045-bolt-lastmodified-header.md).
- Checksum spec and pull request: `openspec/changes/s3-integrity-checksums`
  (PR #902).