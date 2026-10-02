---
title: 'S3 Gateway Core: XML, Buckets, Objects, Lists'
date: 2026-08-11 18:25:16+00:00
draft: false
post_type: architecture
tags:
- go
- s3
- gateway
- xml
- sentinel
categories:
- s3
summary: 'The S3 gateway: SigV4, S3-compliant XML errors, Head/List/Copy, pagination,
  ranges, and metadata — a compatible API surface for real clients.'
artifacts:
- type: pr
  id: '782'
- type: pr
  id: '783'
- type: pr
  id: '785'
- type: spec
  path: openspec/changes/add-s3-protocol
related:
- 006-pluggable-storage-backends
- 009-s3-multipart-and-breadth
- 010-s3-auth-presigned-sigv4
- 012-s3-integrity-checksums
- 011-s3-https-tls-enforcement
- 033-s3-501-discipline-bucket-config
- 034-s3-501-discipline-object-subresources
- 035-s3-501-discipline-remaining-ops
- 036-s3-listxml-appendformat-optimization
- 039-signed-payload-sse-s3
- 040-aws-chunked-streaming
---
S3 is the de-facto object-storage API: a handful of HTTP verbs (PUT, GET, HEAD,
DELETE) plus XML documents that describe buckets, objects, and errors. "Speaking
S3" means a stock AWS client — the `aws` CLI, any vendor SDK, `rclone` — can
point at momo with no custom driver. That compatibility bet is what made the
gateway useful, and it is why the gateway is deliberately boring.

The core design decision: the gateway is a **stateless adapter**, not a second
storage server. It keeps no index, no cache, and no separate object model. Each
S3 request maps onto one call against the native store, so the same bytes are
visible through the native protocol, replication, and the filesystem mount. One
content-addressed store, many doors.

## The Real Problem

We wanted real clients to work on day one, but a gateway that only understands
`PUT` and `GET` is a toy. Real tools immediately exercise the long tail: they
probe metadata with `HEAD`, enumerate buckets and objects, resume partial
downloads with byte ranges, send conditional requests, attach content types and
custom metadata, and copy or bulk-delete objects. A missing verb does not fail
loudly — it fails as a confusing parse error deep inside a client.

The second problem was errors. Go's default is to write a `text/plain` body and
an HTTP status. S3 clients do not read `text/plain`; they parse an XML error
document with a machine-readable code such as `NoSuchKey` or
`BucketAlreadyExists`. Without that XML a client cannot tell "not found" from
"permission denied" from "malformed request", so it retries the wrong things or
gives up silently.

## What We Built

The first wave covered the verbs clients actually call:

- **S3-shaped XML errors.** Every failure returns the error XML an SDK expects,
  with a code and a message, instead of an opaque plaintext body.
- **`HeadObject` and `HeadBucket`.** The `HEAD` verb returns the same headers as
  `GET` — size, content type, ETag, custom metadata — with an empty body, so a
  client can inspect an object without downloading it.
- **Bucket management.** `CreateBucket`, `DeleteBucket`, and `ListBuckets`.
- **`ListObjectsV2` with pagination.** Large buckets are enumerated in pages
  using a continuation token, the opaque cursor the client passes back to fetch
  the next page. One subtle bug here is worth remembering: a page's key count
  must not include common prefixes (the "folders" in a delimiter listing), or a
  client's progress arithmetic drifts.
- **Range and conditional requests.** A byte range such as `bytes=0-1023` lets a
  download resume; conditional headers such as `If-None-Match` let caches and
  sync tools avoid transferring unchanged objects.
- **Object metadata.** `Content-Type` and custom `x-amz-meta-*` headers
  round-trip through the store.
- **`CopyObject` and batch `DeleteObjects`.** Server-side copy and the bulk
  delete document tools use to clean up many keys at once.

## Why an Adapter, Not a Server

Because the gateway owns no state, compatibility work stays contained. Adding an
S3 verb is a translation, not a new data path; durability, replication, and
integrity guarantees are inherited from the store. It also means we never have to
reconcile "the S3 copy" with "the native copy" — there is only one copy.

The trade-off is honesty about scope. Anything S3 specifies that the native store
does not model is rejected rather than emulated, which is the subject of the
501-discipline work ([009](009-s3-multipart-and-breadth.md)). And because S3 is
the widest untrusted surface in the system, every new verb is also new attack
surface.

## ⚡ Bolt & 🛡 Sentinel

🛡 **Sentinel.** S3 was the widest untrusted surface we had. The same month
produced a security sweep ([015](015-sentinel-security-audit.md)) that found HTTP
request smuggling, header-read limit leaks, missing body closes, and CRLF
injection in and around the gateway; all were fixed as part of the hardening arc.
Inbound TLS enforcement followed ([011](011-s3-https-tls-enforcement.md)).

⚡ **Bolt.** The list handlers were the most allocation-heavy code in the
gateway, so a series of allocation optimizations attacked exactly there
([024](024-bolt-performance-engineering.md)).

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Sibling posts: [006: Pluggable Storage Backends](006-pluggable-storage-backends.md),
  [004: Content-Addressable Store](004-cas-content-addressable-store.md),
  [009: Multipart and Protocol Breadth](009-s3-multipart-and-breadth.md),
  [010: SigV4 and Presigned URLs](010-s3-auth-presigned-sigv4.md),
  [011: HTTPS/TLS Enforcement](011-s3-https-tls-enforcement.md),
  [012: Integrity Checksums](012-s3-integrity-checksums.md).
- 501 discipline: [033](033-s3-501-discipline-bucket-config.md),
  [034](034-s3-501-discipline-object-subresources.md),
  [035](035-s3-501-discipline-remaining-ops.md).
- Performance follow-ups: [036](036-s3-listxml-appendformat-optimization.md),
  [040](040-aws-chunked-streaming.md).
- The gateway spec and pull requests: `openspec/changes/add-s3-protocol`
  (PRs #782, #783, #785).