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
momo originally assumed that blob bytes and blob metadata lived on the same
machine, in the same local directory. That assumption was fine until the project
grew an S3-compatible gateway and remote replication. At that point the *placement
of bytes* had to diverge from the *node holding the metadata* — and we did not
want that divergence to require rewriting the storage core.

The storage layer therefore exposes one interface and a single configuration
switch. A **backend** is simply the thing that stores and retrieves opaque blob
bytes. A **seam** is a narrow, compile-time interface we can substitute without a
plugin runtime: no shared library, no RPC hop, no serialization boundary.

The `Store` interface stays stable, and a `[storage] backend` config value
chooses the implementation:

| Backend | Notes |
|---|---|
| `local` | filesystem path, default |
| `nfs` | shared filesystem volume |
| `s3` | remote object store over SigV4, TLS-gated (see [011](011-s3-https-tls-enforcement.md)) |
| `raw` | block-device direct I/O |

Local metadata stays per-node; only the blob bytes route through the chosen
backend.

{{< diagram src="/diagrams/04-storage-backends.svg" alt="Pluggable storage backends architecture" caption="Pluggable storage backends architecture" >}}

## The seam, not the plugin

Backends are **compiled in and selected by declarative config** — the embryo of
the project's *seam-over-plugins* principle. There is no external `.so` to load
and no RPC plugin protocol to negotiate. The blob interface is small and stable
(`PutBlob`/`GetBlob`/`DeleteBlob`/stat/path), and each backend implements it with
its own guarantees. That keeps the trust boundary explicit and the binary
self-contained.

## The trap this avoids

A single hard-coded local blob store was the original design. Once remote
replication arrived, "metadata here, bytes there" had to become a configuration
change rather than a rewrite. Pluggable backends are what made that possible: the
CAS core keeps naming blobs by hash and recording metadata locally, while the
bytes can live on NFS, in an S3 bucket, or on a raw block device.

## ⚡ Bolt lens

- The `GetBlobPath` guard rejects non-local backends, because "a filesystem path"
  is a concept only the local backend can honor.
- Combined metadata reads collapse three separate key/value views into one,
  cutting write-path transactions and CPU (perf work referenced in
  [024](024-bolt-performance-engineering.md)).
- The encryption layer later wrapped the store so end-to-end encryption could
  sit *under* the backend seam transparently
  ([013](013-e2ee-envelope-encryption.md)) — a seam earning its keep.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Configuration reference (the `[storage]` section):
  [docs/CONFIGURATION.md](../../CONFIGURATION.md).
- Spec: `openspec/changes/add-pluggable-storage`; tracking issue #820.
- Related: [004](004-cas-content-addressable-store.md) → this post →
  [008](008-s3-gateway-core.md), [011](011-s3-https-tls-enforcement.md),
  [013](013-e2ee-envelope-encryption.md).
