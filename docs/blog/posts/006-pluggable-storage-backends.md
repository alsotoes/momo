---
title: "Pluggable Storage Backends: local, nfs, s3, raw"
date: 2026-07-26T14:33:59Z
draft: false
post_type: architecture
tags: [go, storage, s3, bolt]
categories: [storage]
summary: "The `[storage] backend` seam swaps blob storage (local, nfs, s3, raw) behind one Store interface, keeping CAS metadata local."
artifacts:
  - {type: spec, path: openspec/changes/add-pluggable-storage}
  - {type: issue, id: "820"}
related:
  - 004-cas-content-addressable-store
  - 008-s3-gateway-core
  - 013-e2ee-envelope-encryption
---
The storage layer exposes one **`Store` interface** and a `[storage] backend`
config switch: `local` (default), `nfs`, `s3` (zero-dep SigV4 client), and
`raw` (direct block I/O). Local bbolt metadata stays per-node; only the blob
bytes route through the chosen backend.

## The seam, not the plugin

Backends are **compiled-in and selected by declarative config** — the embryo of
**Rule 74 (Seam-Over-Plugins)**. No external `.so`, no RPC. The blob interface
is stable (`PutBlob`/`GetBlob`/`DeleteBlob`/stat/path), and each backend
implements it with its own guarantees:

| Backend | Notes |
|---|---|
| `local` | filesystem path, default |
| `nfs` | shared filesystem volume |
| `s3` | remote object store over SigV4, TLS-gated (see [011](011-s3-https-tls-enforcement.md)) |
| `raw` | block-device direct I/O |

## The trap this avoids

A single hard-coded `local` blob store was the original assumption. Once momo
gained an S3 gateway and remote replication, the *placement of bytes* had to
diverge from the *metadata node* — pluggable backends made "metadata here, bytes
there" a config change rather than a rewrite. See `docs/CONFIGURATION.md`
(`[storage]` section).

## ⚡ Bolt lens

- The `GetBlobPath` guard rejects non-local backends (a `local`-only concept).
- Combined metadata reads collapse 3 bbolt views into 1 (perf work referenced in
  [024](024-bolt-performance-engineering.md)).
- The encryption layer later wrapped the store so E2EE could sit *under* the
  backend seam transparently ([013](013-e2ee-envelope-encryption.md)).

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

[004](004-cas-content-addressable-store.md) → this post → [008](008-s3-gateway-core.md),
[011](011-s3-https-tls-enforcement.md), [013](013-e2ee-envelope-encryption.md).