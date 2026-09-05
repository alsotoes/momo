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
Large S3 uploads can't be buffered whole for signing — memory explodes and latency spikes. `aws-chunked` payload encoding solves this by streaming the body as a sequence of individually-signed chunks.

## The Problem

- SigV4 signs the payload hash; verifying a multi-GB upload requires the whole body
- Buffering the full body violates momo's bounded-memory ⚡ Bolt principle
- Chunked framing exists precisely to fix this: sign each chunk, verify incrementally

## The Solution: `STREAMING-AWS4-HMAC-SHA256-PAYLOAD`

`S3Communicator` now decodes `aws-chunked` payloads:

```
header: x-amz-content-sha256: STREAMING-AWS4-HMAC-SHA256-PAYLOAD
body:   [chunk-size][chunk-signature][chunk-data]...
```

- Each chunk carries its own SigV4 signature (`x-amz-chunk-signature`)
- Server streams each chunk through the hasher, verifying the signature before accepting
- No full-body buffering — memory bounded to one chunk
- Per-chunk integrity: corruption/mismatch → `400 XAmzContentSHA256Mismatch` at the exact chunk

## Implementation

- `decodeStreamingPayload` in `s3_communicator.go` parses chunk framing
- Per-chunk SigV4 signature verification (chaining signatures: chunk N signs hash of chunk N-1)
- Bounded buffer: single chunk held, not the whole body
- Coexists with checksum verification (032) — both run per upload

## Verification

- aws-chunked PUT streams a large body without OOM (bounded chunk buffer)
- Corrupted chunk → 400 at the offending chunk, connection torn down
- Standard (non-chunked) PUT unaffected
- Cross-checked against aws-cli/`AWS4-HMAC-SHA256-PAYLOAD` behavior

## Standards

Per [docs/STANDARDS.md](../../STANDARDS.md): ⚡ **Bolt** (bounded memory, streaming, no buffering), 🛡 **Sentinel** (per-chunk integrity, honest mismatch errors).

## Artifacts

- Spec: `openspec/changes/aws-chunked-streaming/`
- Issue: #773
- PR: #... (merged)
