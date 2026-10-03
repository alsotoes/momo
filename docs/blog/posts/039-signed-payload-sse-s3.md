---
title: "Signed Payloads and Server-Sent Events for S3"
date: 2026-08-11T05:14:24Z
draft: false
post_type: architecture
tags: [s3, streaming, sigv4, sentinel]
categories: [s3]
summary: "SigV4 signed payload verification for S3 PUT/POST and Server-Sent Events for async operation notifications."
artifacts:
  - {type: spec, path: openspec/changes/signed-payload-and-sse}
  - {type: issue, id: "776"}
related:
  - 010-s3-auth-presigned-sigv4
  - 008-s3-gateway-core
  - 051-bolt-aws-chunked-zero-alloc
  - 040-aws-chunked-streaming
---
S3 requests are signed, and the signature is supposed to cover the request body.
For a while ours covered the *claim* about the body but not the body itself. In
parallel, we added a way for long-running operations to talk back to the client
without the client having to poll. Two complementary pieces: closing an integrity
gap, and opening a notification channel.

## The Real Problem

**Signing.** AWS Signature Version 4 (SigV4) is the scheme S3 clients use to prove
a request is authentic and unmodified. Part of the signature is a hash of the
request body, sent in the `x-amz-content-sha256` header. A server that verifies
the signature but never recomputes that hash is trusting a claim it never checks:
an attacker — or a buggy proxy — can swap the body while leaving the signed header
intact.

That was our gap. We accepted the signed header and did not verify the payload.

**Notifications.** Some operations take a long time: completing a large multipart
upload, deleting thousands of objects. Polling for status wastes requests and adds
latency. We wanted a way for the server to push progress to a client over a single
long-lived connection.

## Why the Obvious Fixes Failed

**"Verify the body by reading it all first."** That means buffering the entire
upload in memory before checking the hash — exactly the bounded-memory violation
we refuse elsewhere. Verification has to happen *while* streaming.

**"Skip the hash and rely on TLS."** TLS protects the connection, not the
application-layer contract. A client that signs a body is entitled to have that
body checked, even against an honest-but-buggy intermediary.

**"Poll for status."** It works, but it is the wrong shape for a server that
already holds the operation's state. A push channel is cheaper for both sides.

## The Solution

### Signed payload verification

When a request arrives, the server captures the declared body hash — either the
plain `x-amz-content-sha256` value or the streaming variant
`STREAMING-AWS4-HMAC-SHA256-PAYLOAD`, which signals that the body is framed in
signed chunks (covered in the companion post). The body is then streamed through a
hasher as it is read. When the body ends, the computed hash is compared with the
declared one:

- **Match** → the request proceeds.
- **Mismatch** → `400` with the `XAmzContentSHA256Mismatch` error code.

Because the hash is computed during streaming, a multi-gigabyte body never has to
be held in memory. The integrity check and the memory bound are not in conflict.

### Server-Sent Events

Server-Sent Events (SSE) is a simple HTTP mechanism for one-way server-to-client
streaming: the server responds with `Content-Type: text/event-stream` and writes a
sequence of events until it closes the connection. The client reads them as they
arrive; there is no polling.

We use it to report progress for asynchronous operations. Each event carries the
operation's identifier, its status, and its progress, so a client can follow a
long-running job on one connection.

## How We Verified

- A signed request whose body does not match its declared hash returns `400`
  `XAmzContentSHA256Mismatch`.
- A validly signed payload is accepted.
- The signed-chunk path verifies each chunk, preserving per-chunk integrity.
- An SSE client connects, receives events, and sees the stream close cleanly.

## Failure Modes

| Risk | Guard |
|------|-------|
| A large body forcing a buffer | The hash is computed during streaming; only one chunk is held |
| A streaming request bypassing verification | The streaming content-hash value selects the chunked verifier, not a skip path |
| A client unable to tell when a job is done | The server closes the stream when the operation completes |

## When NOT to Use This

- **Non-S3 traffic.** These are S3 compatibility features; the SSE channel is for
  operations that actually run asynchronously.
- **Requests with no body.** There is no payload hash to verify on a `GET`.

## Engineering Standards (🛡 Sentinel)

Per [docs/STANDARDS.md](../../STANDARDS.md): 🛡 **Sentinel** — verify what you
sign, with no trust gaps and an honest mismatch error.

## References / Dig deeper

- Spec: `openspec/changes/signed-payload-and-sse/`.
- Issue: #776.
- Related posts: [008: S3 Gateway Core](008-s3-gateway-core.md),
  [010: S3 Auth, Presigned URLs, and SigV4](010-s3-auth-presigned-sigv4.md),
  [040: aws-chunked Streaming](040-aws-chunked-streaming.md).
