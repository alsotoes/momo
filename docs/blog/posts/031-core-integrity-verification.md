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
  - 041-architecture-decision-records
  - 043-reduce-read-verify-hashing
---

When support for AWS S3 additive integrity checksums (`x-amz-checksum-crc32`, `crc32c`, `sha1`, `sha256`) initially shipped in PR #902, the verification logic was implemented directly inside `src/transport/s3_communicator.go`. While functional for standard AWS SDK clients, this created a dangerous architectural flaw: **integrity was treated as a transport feature rather than a storage invariant**.

If an upload bypassed the S3 HTTP layer—arriving instead via momo's native binary TCP or QUIC protocols, or forwarded across nodes during cluster replication—integrity validation was skipped entirely. Even worse, if a corrupted primary node accepted bad bytes, it would happily replicate those corrupted bytes to secondary nodes, poisoning the entire cluster.

In issue #903 and PR #911, momo refactored this model around the **Commodity Protocol Principle**: transport adapters are disposable commodities; all integrity verification must reside in the centralized storage ingest core.

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

---

## 1. The Protocol-Agnostic Seam: `ChecksumProvider`

To decouple the core ingest pipeline from S3-specific headers and status codes, `src/common/checksum.go` introduces a generic checksum representation:

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
```

The server ingest routine in `src/server/file.go` queries the active transport connection for the `ChecksumProvider` interface:

```go
type ChecksumProvider interface {
    ChecksumExpectations() common.ChecksumSet
    OnChecksumMismatch(err error) error
}
```

During `getFile()`, the server reads from the network socket through an `io.TeeReader` that feeds both the physical blob storage and the streaming hash engines simultaneously. Once the stream terminates:

```go
// server/file.go
if provider, ok := comm.(transport.ChecksumProvider); ok {
    expectations := provider.ChecksumExpectations()
    if len(expectations) > 0 {
        if err := verifier.Verify(expectations); err != nil {
            // Rollback: immediately purge partially written blob
            _ = s.store.Delete(meta.Hash)
            return provider.OnChecksumMismatch(err)
        }
    }
}
```

If a mismatch is detected, the store immediately deletes the uncommitted blob from disk and calls `OnChecksumMismatch`. The S3 adapter translates this error into an AWS-compliant `400 BadDigest` XML error, while native TCP/QUIC connections return `syscall.EBADMSG`.

---

## 2. Cluster-Wide Protection: Peer Re-Verification

A critical vulnerability in distributed storage is replica trust: Node A receives data, verifies it, and forwards it to Node B. If Node A's memory or network interface is faulty, Node B blindly accepts corrupted bytes.

In momo's centralized architecture, Node A encodes the expected checksum manifest into a base64 header:
```
X-Momo-S3-Meta: {"checksums":[{"algo":"CRC32C","val":"4a2f8b=="}]}
```
When Node B receives the forwarded replication stream, its own `getFile()` pipeline extracts the header, initializes independent streaming hashers, and verifies the incoming bytes against the original client manifest. Corrupted nodes are isolated before bad data can spread.

---

## 3. Tradeoff Analysis: Surface vs. Centralized Core Verification

| Dimension | Surface-Level Verification (Old) | Centralized Core Ingest (New) |
|---|---|---|
| **Architectural Purity** | Coupled: Transport layers dictate storage logic | **Decoupled**: Core enforces invariants; transports are adapters |
| **Multi-Protocol Safety** | Only S3 uploads were protected | **All protocols** (S3, TCP, QUIC, Replicas) are protected |
| **Cluster Blast Radius** | Corrupt primary poisons all secondary replicas | **Zero-trust**: Replicas independently verify every byte |
| **Rollback Reliability** | Ad-hoc cleanup left orphaned temp files | **Guaranteed atomicity**: Immediate `store.Delete` on mismatch |
| **CPU Overhead** | $O(1)$ pass on primary only | $O(1)$ single-pass pipeline executed concurrently across replicas |

---

## 4. Engineering Standards

In accordance with [docs/STANDARDS.md](../../STANDARDS.md):
- ⚡ **Bolt (Zero Extra Copies)**: `io.TeeReader` computes CRC32/SHA256 digests in-flight as network buffers pass directly into disk storage. No intermediate memory buffer or temporary spool file is required.
- 🛡 **Sentinel (Fail-Closed Core)**: Core ingest never trusts transport guarantees. Any digest mismatch triggers an immediate rollback and purges the blob address before it can be indexed into Bbolt.

## Related

- S3 checksum specification: [012](012-s3-integrity-checksums.md)
- Storage verify-on-read & GC: [007](007-at-rest-integrity-and-gc.md)
- Content-addressable foundation: [004](004-cas-content-addressable-store.md)
- ADR framework: [041](041-architecture-decision-records.md)
- Read verification optimization: [043](043-reduce-read-verify-hashing.md)
