---
title: "AWS Chunked Streaming: Signed Payloads Without Buffering"
date: 2026-08-11T04:52:58Z
draft: false
post_type: architecture
tags: [s3, streaming, sigv4, bolt, sentinel]
categories: [s3, performance]
summary: "aws-chunked payload encoding streams large S3 uploads in signed chunks — no full-body buffering, per-chunk SigV4 integrity."
artifacts:
  - {type: spec, path: openspec/changes/aws-chunked-streaming}
  - {type: issue, id: "773"}
related:
  - 010-s3-auth-presigned-sigv4
  - 039-signed-payload-sse-s3
  - 008-s3-gateway-core
---
A signed upload has a chicken-and-egg problem: the signature covers the body, but
you cannot know the body's hash until you have read it all, and reading it all
means buffering it. For a multi-gigabyte object that is not an option. The S3
protocol's answer is `aws-chunked` encoding, and this is how we implemented it.

## The Real Problem

SigV4 signs a hash of the request body. To verify that hash, a server would
normally need the complete body. For a small object that is fine; for a
multi-gigabyte upload it means holding the whole thing in memory before accepting
a single byte — which breaks momo's rule that memory stays bounded no matter how
large the object.

`aws-chunked` is a payload encoding that solves this. Instead of one opaque body,
the request carries a sequence of chunks, each preceded by its own size and its
own signature. The server can verify and accept one chunk at a time, never holding
more than a single chunk.

## Why the Obvious Fixes Failed

**"Buffer it."** The whole point of a streaming store is not to.

**"Skip verification for large uploads."** That reintroduces exactly the trust gap
we closed elsewhere: a signed request whose body is never checked.

**"Verify only at the end."** By then the bytes have been written. Detecting
corruption after the fact means undoing work, and it means trusting the client for
the duration of the upload.

## The Solution: `STREAMING-AWS4-HMAC-SHA256-PAYLOAD`

A request opts into chunked signing by declaring a special content hash:

```
header: x-amz-content-sha256: STREAMING-AWS4-HMAC-SHA256-PAYLOAD
body:   [chunk-size][chunk-signature][chunk-data] ...
```

The server decodes that framing and verifies each chunk before accepting it:

- Each chunk carries its own SigV4 signature.
- The signatures are **chained**: the signature of chunk N covers the hash of
  chunk N-1. Dropping, reordering, or truncating chunks breaks the chain, so the
  server notices at the first affected chunk.
- A mismatch returns `400` with `XAmzContentSHA256Mismatch`, at the exact chunk
  where the problem appears, and the connection is torn down.
- Memory stays bounded to one chunk.

## How We Verified

- An `aws-chunked` `PUT` streams a large body without running out of memory; the
  server holds a single chunk at a time.
- A corrupted chunk returns `400` at the offending chunk and the connection is
  closed.
- A standard, non-chunked `PUT` is unaffected.
- The behavior was cross-checked against `aws-cli` and the
  `AWS4-HMAC-SHA256-PAYLOAD` scheme.

## Failure Modes

| Risk | Guard |
|------|-------|
| A corrupted chunk being accepted | Each chunk is verified before its data is accepted; a mismatch returns `400` |
| Reordered or dropped chunks | Signatures are chained, so a broken chain is detected at the next chunk |
| The whole body being buffered | Only one chunk is held at a time |
| Non-chunked uploads being affected | The streaming content-hash value gates the chunked path |

## When NOT to Use This

- **Small uploads.** If the whole body fits comfortably in memory, ordinary
  signing is simpler.
- **Clients that do not send chunked framing.** The server detects the streaming
  content-hash value; without it, the standard path applies.

## Engineering Standards (⚡ Bolt & 🛡 Sentinel)

Per [docs/STANDARDS.md](../../STANDARDS.md):

- ⚡ **Bolt**: bounded memory. The server never buffers more than one chunk.
- 🛡 **Sentinel**: per-chunk integrity with honest, precise mismatch errors.

## References / Dig deeper

- Spec: `openspec/changes/aws-chunked-streaming/`.
- Issue: #773.
- Related posts: [008: S3 Gateway Core](008-s3-gateway-core.md),
  [010: S3 Auth, Presigned URLs, and SigV4](010-s3-auth-presigned-sigv4.md),
  [039: Signed Payloads and SSE](039-signed-payload-sse-s3.md).
