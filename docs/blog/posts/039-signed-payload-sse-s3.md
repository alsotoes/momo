---
title: "Signed Payloads and Server-Sent Events for S3"
date: 2026-08-11T05:14:24Z
draft: false
tags: [s3, streaming, sigv4, sentinel]
categories: [s3]
summary: "SigV4 signed payload verification for S3 PUT/POST and Server-Sent Events for async operation notifications."
artifacts:
  - {type: spec, path: openspec/changes/signed-payload-and-sse}
  - {type: issue, id: "776"}
related:
  - 010-s3-auth-presigned-sigv4
  - 008-s3-gateway-core
  - 040-aws-chunked-streaming
---
Two complementary additions round out S3 compatibility: **SigV4 signed payload verification** (integrity of request bodies) and **Server-Sent Events** (async operation notifications).

## Signed Payloads

AWS SigV4 supports signing the request body hash (`x-amz-content-sha256`). Previously the header was accepted but the payload hash wasn't verified — a **trust gap**: a signed header with an unsigned/mismatched body could pass.

**Fix**: `S3Communicator` now verifies the payload hash against the actual streamed body:

- `x-amz-content-sha256` (or the new `STREAMING-AWS4-HMAC-SHA256-PAYLOAD`) is captured at request read
- Body is streamed through a hasher; on completion, hashes are compared
- **Mismatch** → `400 XAmzContentSHA256Mismatch`
- `aws-chunked` framing keeps per-chunk signing

This closes the integrity gap — a validly-signed request can no longer carry an arbitrary body.

## Server-Sent Events

Long-running async operations (e.g., large multipart completes, batch deletes) can stream progress/notifications to the client over SSE:

- `Content-Type: text/event-stream`
- Events carry operation id, status, and progress
- Client can subscribe without polling

## Verification

- Signed-payload mismatch → `400 XAmzContentSHA256Mismatch`
- Valid signed payload → accepted
- `aws-chunked` per-chunk verification intact
- SSE stream: connect, receive events, close cleanly

## Standards

Per [docs/STANDARDS.md](../STANDARDS.md): 🛡 **Sentinel** (verify what you sign — no trust gaps, honest mismatch errors).

## Artifacts

- Spec: `openspec/changes/signed-payload-and-sse/`
- Issue: #776
- PR: #... (merged)
