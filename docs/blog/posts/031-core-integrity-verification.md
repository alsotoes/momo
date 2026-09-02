---
title: "Centralized Integrity Verification: Checksums Move to the Storage Core"
date: 2026-08-24T18:03:31Z
draft: false
tags: [integrity, checksums, storage, bolt, sentinel]
categories: [storage]
summary: "S3 integrity checksums moved from a surface-level adapter into the storage/ingest core via a protocol-agnostic ChecksumProvider seam — every write path verified, replicas re-verify on receive."
artifacts:
  - {type: spec, path: openspec/changes/core-integrity-verification}
  - {type: issue, id: "903"}
related:
  - 012-s3-integrity-checksums
  - 007-at-rest-integrity-and-gc
  - 004-cas-content-addressable-store
  - 041-architecture-decision-records
  - 043-reduce-read-verify-hashing
---
# Centralized Integrity Verification

The S3 `x-amz-checksum-*` headers originally lived only in the S3 adapter — a surface-level check that didn't protect data written via momo-native protocols. The fix: centralize integrity verification in the storage/ingest core via a protocol-agnostic `ChecksumProvider` seam.

## The Problem

- S3 `x-amz-checksum-*` verification lived only in `s3_communicator.go`
- momo-native protocols (TCP/QUIC) had no checksum verification
- Replicas never re-verified incoming data
- Checksum logic duplicated across protocols

## The Solution: Centralized `ChecksumProvider` Seam

Created a protocol-agnostic verification layer in `src/common/checksum.go`:

```go
// ChecksumRef — single algorithm + value per object
type ChecksumRef struct {
    Algorithm string // CRC32C, SHA256, etc.
    Value     []byte
}

// ChecksumSet — multiple checksums per object (future extensibility)
type ChecksumSet []ChecksumRef

// VerifyStream — streaming verification wrapper
func (c *ChecksumSet) VerifyStream(r io.Reader) error { ... }
```

**Protocol-agnostic seam**: `transport.ChecksumProvider` (replaces `ChecksumFinalizer`):
```go
type ChecksumProvider interface {
    StartChecksum() (ChecksumSet, func(ChecksumSet) error)
    VerifyAndFinalize(ChecksumSet) error
}
```

`getFile` (in `server/file.go`) now:
1. Asserts `ChecksumProvider` on the transport
2. Streams data → hasher → storage
3. Calls `VerifyAndFinalize` → on mismatch: `400 BadDigest` + delete object
4. S3 error encoding (400 BadDigest) stays in S3 adapter — **core is protocol-agnostic**

## S3 Integration

- `collectS3Headers` captures `x-amz-checksum-*` + `x-amz-checksum-algorithm`
- `parseChecksum` resolves single algorithm (CRC32/CRC32C/SHA1/SHA256); 400 on conflict/unknown
- **PUT Path**: Verify after `store.Put` (single-part) / before `store.Put` (multipart); fail → 400 BadDigest + delete
- **GET**: Compute digest, append after `appendS3MetaHeaders` on 200/206
- **Multipart**: Verify in `CompleteMultipartUpload` before `store.Put`
- **Propagation**: `X-Momo-S3-Meta` header (base64 JSON) carries checksums to replicas

## Replica Re-Verification

Replicas receive `X-Momo-S3-Meta` header → decode → re-verify on receive via same `getFile` path. No silent corruption propagation.

## CASStore VerifyChecksum

Opt-in `CASStore.VerifyChecksum` (concrete method, doesn't break `Store` mockers) for at-rest verification on read.

## Verification

- Unit tests: parseChecksum, VerifyAndFinalize, multipart, GET checksum-mode, unknown algo
- Integration: server single-part GET/PUT with real CAS store
- S3 checksum-mode GET returns base64 checksum correctly (exact-case assertion)

## Standards

Per [docs/STANDARDS.md](../STANDARDS.md): ⚡ **Bolt** (single-pass streaming hasher, zero extra copies), 🛡 **Sentinel** (fail-closed: 400 BadDigest, delete on mismatch, core never trusts surface).

## Follow-ups

- Phase 2c: Replica re-verify satisfied via centralized `getFile` + forwarded headers
- Phase 3: `CASStore.VerifyChecksum` opt-in at-rest scrub
- Phase 5: `ARCHITECTURE.md` parity

## Artifacts

- Spec: `openspec/changes/core-integrity-verification/`
- Issue: #903
- PR: #... (merged)
