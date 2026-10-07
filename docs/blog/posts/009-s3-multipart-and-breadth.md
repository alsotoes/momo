---
title: "S3 Multipart and Protocol Breadth: 501 Discipline"
date: 2026-08-13T06:02:26Z
draft: false
post_type: architecture
tags: [go, s3, multipart, protocol, sentinel]
categories: [s3]
summary: "Multipart upload support plus a rigorous 501 strategy for unsupported S3 subresources — honest breadths over silent misbehavior."
artifacts:
  - {type: pr, id: "801"}
  - {type: pr, id: "913"}
  - {type: pr, id: "915"}
  - {type: pr, id: "921"}
  - {type: spec, path: openspec/changes/s3-multipart-upload}
  - {type: spec, path: openspec/changes/s3-p5-remaining-subresource-501}
related:
  - 008-s3-gateway-core
  - 012-s3-integrity-checksums
  - 033-s3-501-discipline-bucket-config
---
S3's surface is enormous, and we had a choice about how to grow into it: pretend
to support everything, or support a precise subset and say so loudly. We chose
the second, and it shaped both halves of this post — multipart upload, which we
added for real, and a disciplined `501 Not Implemented` response for everything
else.

## The Real Problem

Two different gaps kept showing up. The first was concrete. Large-file tooling
does not upload a multi-gigabyte object in one request; it uses **multipart
upload**, a three-step dance where the client opens an upload, sends the object
as numbered parts, then asks the server to assemble the parts into one object.
Without it the biggest and most common S3 workload — copying a big file — simply
did not work.

The second gap was philosophical. Clients probe capabilities we never intended to
implement: bucket lifecycle rules, versioning, CORS, per-object access control
lists, tagging, and dozens of other "subresources" — the sub-paths that hang off
a bucket or object URL. The tempting answer is to return `200 OK` and do nothing.
That is a trap. A client that believes its lifecycle rule was accepted will
behave as if it were, and corrupted assumptions are far worse than an error.

## Multipart Upload

{{< diagram src="/diagrams/11-s3-multipart.svg" alt="S3 multipart upload flow" caption="S3 multipart upload flow" >}}

The flow is `CreateMultipartUpload` → `UploadPart` (repeated) →
`CompleteMultipartUpload`, with `AbortMultipartUpload` to discard a half-finished
upload. Each part is stored as it arrives; at completion the parts are
concatenated into a single content-addressed blob, so the finished object is
indistinguishable from one written in a single request. This is what large-object
tooling actually calls, and it closed the biggest "S3-shaped" hole in the gateway
core ([008](008-s3-gateway-core.md)).

## 501 Discipline

`501 Not Implemented` is the HTTP status meaning "this server does not support
the functionality required to fulfil the request". We made it the honest default
for subresources momo deliberately does not do:

- **Bucket-configuration subresources** — lifecycle, versioning, CORS, and
  friends — return `501`.
- **Object-level subresources** — ACLs, tagging, and friends — return `501`.
- **Everything remaining** returns `501` from a documented, synchronized catalog,
  so no operation quietly pretends to succeed.

Why not a best-effort `200`? Because a silent partial implementation corrupts
client assumptions far worse than an explicit rejection. The Sentinel mindset is
**fail closed, loudly**: an operation we cannot honour must not look like it
succeeded.

> **Pattern: Fail Closed, Loudly**
> When a protocol exposes operations you do not implement, return that protocol's
> explicit "not implemented" signal rather than a success code. A success code is
> a promise; if you cannot keep it, do not make it.
>
> **Applies when**: a partial protocol implementation faces clients that probe
> for capabilities you have no plans to build.
> **Doesn't apply**: operations you intend to implement soon — there, a `501` is
> a temporary honest placeholder, not a permanent answer.

## ⚡ Bolt & 🛡 Sentinel

⚡ **Bolt.** `ListParts` (the XML listing of an in-progress multipart upload) was
a standout allocation hotspot. It was profiled and de-allocated in the Bolt arc
([024](024-bolt-performance-engineering.md)), and part listings reuse the same
pagination discipline as `ListObjectsV2`.

🛡 **Sentinel.** The 501 catalog is a safety property: the set of supported
operations is explicit, synchronized, and testable, so "unsupported" can never
masquerade as "done".

See [docs/CORE/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Core gateway: [008](008-s3-gateway-core.md). Integrity echo:
  [012](012-s3-integrity-checksums.md).
- 501 discipline in depth: [033](033-s3-501-discipline-bucket-config.md).
- Compatibility matrix and the 501 catalog:
  `docs/REFERENCE/COMPATIBILITY.md`,
  `openspec/changes/s3-p5-remaining-subresource-501/`.
- Multipart spec and pull requests: `openspec/changes/s3-multipart-upload`
  (PRs #801, #913, #915, #921).