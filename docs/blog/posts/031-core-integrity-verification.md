---
title: "Centralized Integrity Verification: Checksums Move to the Storage Core"
date: 2026-08-24T18:03:31Z
draft: false
post_type: architecture
tags: [integrity, checksums, storage, bolt, sentinel]
categories: [storage]
summary: "S3 integrity checksums moved from a surface-level adapter into the storage/ingest core via a protocol-agnostic ChecksumProvider seam — every write path verified, replicas re-verify on receive."
artifacts:
  - {type: spec, path: openspec/changes/core-integrity-verification}
  - {type: issue, id: "903"}
  - {type: pr, id: "911"}
related:
  - 012-s3-integrity-checksums
  - 007-at-rest-integrity-and-gc
  - 004-cas-content-addressable-store
  - 043-reduce-read-verify-hashing
  - 055-adaptive-volume-storage
---

A **checksum** is a short fingerprint of a stream of bytes: recompute it on the
other side and you know whether the data survived the trip. When we first
supported AWS S3's additive integrity checksums, we implemented the verification
inside the S3 transport adapter — the code that speaks the S3 HTTP protocol. It
worked, for S3 clients. It was also a trap.

The trap: **integrity was treated as a transport feature rather than a storage
invariant.** If an upload arrived over momo's native binary protocol, or was
forwarded between nodes during replication, it bypassed the S3 layer entirely —
and with it, every integrity check. Worse, a corrupted primary node would happily
replicate its corrupted bytes to secondary replicas, spreading the poison across
the cluster. Verification that only runs on one of several front doors is not
verification; it is a coincidence.

## The Real Problem

Storage systems have many doors. S3 clients come in through HTTP; native clients
come in over a binary protocol; other nodes come in during replication. If each
door decides for itself whether to check integrity, then the guarantee depends on
which door you used. We wanted a single, unavoidable checkpoint: no matter how
bytes enter, they are verified before they are committed.

## Why the Obvious Solutions Failed

**"Add the check to each transport."** This is duplication with a drift problem.
Each adapter re-implements the same logic against different header names and
status codes, and the next protocol someone adds silently misses the check.

**"Trust the primary to verify before replicating."** This makes every replica
trust the primary's memory and network card. A single faulty node then becomes a
cluster-wide data-corruption event.

**"Buffer the upload, verify, then store."** Correct, but it doubles memory and
latency: you hold the whole object before committing it.

## The Solution: Verification at the Core, in One Pass

We adopted a principle we call the **Commodity Protocol Principle**: transport
adapters are disposable commodities, and all integrity verification belongs in
the centralized storage ingest core. The core defines a protocol-agnostic
**seam** — a small interface that any transport can implement to describe what
checksums it expects and how to report a mismatch.

```
========================================================================================
                          CENTRALIZED CORE INGEST ARCHITECTURE
========================================================================================

    [ S3 Client (aws-cli) ]       [ Native TCP Client ]       [ Peer Node (Replication) ]
               |                            |                             |
     (x-amz-checksum-sha256)                |                  (X-Momo-S3-Meta header)
               |                            |                             |
               v                            v                             v
     [ S3Communicator ]             [ MomoTCPComm ]               [ Peer Ingest Comm ]
               |                            |                             |
               +----------------------------+-----------------------------+
                                            |
                                            v  Implements transport.ChecksumProvider
                         +-------------------------------------+
                         |      server/file.go: getFile()      |
                         |      Central Storage Ingest Seam    |
                         +-------------------------------------+
                                            |
                         +------------------+------------------+
                         |                                     |
                         v                                     v
               [ Stream to Hasher ]                  [ Stream to CASStore ]
               CRC32, CRC32C, SHA256                 blobs/e3/b0/...
                         |                                     |
                         +------------------+------------------+
                                            |
                                            v
                                  Check: Matches Digest?
                                  /                    \
                            [ YES ]                   [ NO ]
                               |                         |
                               v                         v
                       Commit Metadata           1. Abort stream
                       Return HTTP 200 OK        2. CASStore.Delete(hash)
                                                 3. Return 400 BadDigest / EBADMSG
```

Concretely, the server's ingest routine asks the active connection for a
**checksum provider** — the seam. If the connection offers expectations, the core
verifies the incoming stream against them. The stream flows through a tee: one
branch goes to the physical blob store, the other to streaming hash engines that
compute CRC32, CRC32C, SHA-1, and SHA-256 as the bytes arrive. There is no
intermediate buffer and no temporary spool file — the digest is computed in
flight.

```go
type ChecksumAlgorithm string

const (
    AlgoCRC32   ChecksumAlgorithm = "CRC32"
    AlgoCRC32C  ChecksumAlgorithm = "CRC32C"
    AlgoSHA1    ChecksumAlgorithm = "SHA1"
    AlgoSHA256  ChecksumAlgorithm = "SHA256"
)

type ChecksumRef struct {
    Algorithm ChecksumAlgorithm
    Value     []byte
}

type ChecksumSet []ChecksumRef

// The seam every transport implements so the core can verify without
// knowing which protocol delivered the bytes.
type ChecksumProvider interface {
    ChecksumExpectations() common.ChecksumSet
    OnChecksumMismatch(err error) error
}
```

```go
// server/file.go — ingest verifies, then commits or rolls back.
if provider, ok := comm.(transport.ChecksumProvider); ok {
    expectations := provider.ChecksumExpectations()
    if len(expectations) > 0 {
        if err := verifier.Verify(expectations); err != nil {
            // Rollback: immediately purge the partially written blob.
            _ = s.store.Delete(meta.Hash)
            return provider.OnChecksumMismatch(err)
        }
    }
}
```

On a mismatch the core aborts the stream, deletes the partially written blob,
and returns a transport-appropriate error: the S3 adapter translates it into an
AWS-compliant `400 BadDigest`, while native connections return the operating
system's "bad message" code. The rollback is part of the invariant, not an
afterthought.

**Replicas re-verify.** The other half of the design is zero trust between nodes.
When a node forwards a replicated stream, it encodes the expected checksum
manifest into a small metadata header. The receiving node runs the *same* core
ingest pipeline, extracts the manifest, and independently verifies the bytes
against the original client's fingerprint. A faulty sender is caught at the
receiver before the bad data is indexed.

## Tradeoff Analysis: Surface vs. Centralized Core Verification

| Dimension | Surface-Level Verification (Old) | Centralized Core Ingest (New) |
|---|---|---|
| **Architectural Purity** | Coupled: transport layers dictate storage logic | **Decoupled**: core enforces invariants; transports are adapters |
| **Multi-Protocol Safety** | Only S3 uploads were protected | **All protocols** (S3, TCP, QUIC, replicas) are protected |
| **Cluster Blast Radius** | Corrupt primary poisons all secondary replicas | **Zero-trust**: replicas independently verify every byte |
| **Rollback Reliability** | Ad-hoc cleanup left orphaned temp files | **Guaranteed atomicity**: immediate delete on mismatch |
| **CPU Overhead** | Single pass on the primary only | Single pass executed concurrently across replicas |

## Engineering Standards

In accordance with [docs/CORE/STANDARDS.md](../../STANDARDS.md):

- ⚡ **Bolt (Zero Extra Copies)**: the tee computes CRC32/SHA-256 digests in
  flight as network buffers pass directly into disk storage. No intermediate
  memory buffer or temporary spool file is required.
- 🛡 **Sentinel (Fail-Closed Core)**: core ingest never trusts transport
  guarantees. Any digest mismatch triggers an immediate rollback and purges the
  blob address before it can be indexed.

## References / Dig deeper

- Spec: `openspec/changes/core-integrity-verification`.
- Source: `src/common/checksum.go`, `src/server/file.go`,
  `src/transport/s3_communicator.go`.
- Sibling posts: S3 checksum specification [012](012-s3-integrity-checksums.md),
  storage verify-on-read & GC [007](007-at-rest-integrity-and-gc.md),
  content-addressable foundation [004](004-cas-content-addressable-store.md),
  read verification optimization [043](043-reduce-read-verify-hashing.md).
