---
title: "S3 Multipart and Protocol Breadth: 501 Discipline"
date: 2026-08-13T06:02:26Z
draft: false
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
---
# S3 Multipart and Protocol Breadth: 501 Discipline

Two directions grew the S3 surface: **adding** multipart, and **brutally
honest** `501 Not Implemented` responses for what momo deliberately doesn't do
yet.

## Multipart upload (#801)

`CreateMultipartUpload` → `UploadPart` → `CompleteMultipartUpload` / `Abort`,
with the parts assembling at completion into one CAS blob. This is what
large-object tooling actually uses, and it closes the biggest "S3-shaped" gap in
the gateway core ([008](008-s3-gateway-core.md)).

## 501 Discipline (P3–P5, #913/#915/#921)

A *philosophy*, formalized in the S3 501 reject sets:

- **bucket-config subresources** (P3): lifecycle, versioning, CORS, etc. → 501.
- **object-level subresources** (P4): ACLs, tagging, etc. → 501.
- **everything remaining** (P5): a documented, synced 501 catalog.

Why not a "best-effort" 200? Because a silent partial implementation corrupts
client assumptions far worse than an explicit `501`. Sentinel mindset: **fail
closed, loudly**. `docs/COMPATIBILITY.md` and
`openspec/changes/s3-p5-remaining-subresource-501/` are the sync source-of-truth
for what is/ isn't supported (docs-sync discipline via #922/#923).

## ⚡ Bolt lens

ListParts XML encoding was a standout allocation hotspot — profiled and
de-allocated in the Bolt arc ([024](024-bolt-performance-engineering.md)), and
part listings reuse the same pagination discipline as ListObjectsV2.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

Core gateway: [008](008-s3-gateway-core.md). Integrity echo:
[012](012-s3-integrity-checksums.md).