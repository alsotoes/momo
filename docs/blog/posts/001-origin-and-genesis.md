---
title: "Origin: From Replication Playground to Object Store"
date: 2025-09-09T20:10:05Z
draft: false
tags: [go, origin, architecture]
categories: [origin]
summary: "How momo started as a file-replication playground in Go and grew into a distributed content-addressed object store."
artifacts:
  - {type: commit, id: "a8114af4"}
related:
  - 002-replication-strategies-polymorphic
  - 004-cas-content-addressable-store
---
Momo's earliest commit (`a8114af4`, 2025-09-09) is a bare `go.mod` and Docker
scaffolding. Its stated purpose at the time: a **file-replication playground**
in Go demonstrating different replication strategies behind a simple,
metrics-driven controller — the "polymorphic system".

## The spark

The original pitch was deliberately narrow: prove that one daemon could switch
replication strategies at runtime (`ReplicationNone`, `Chain`, `Splay`,
`Primary-Splay`) based on live CPU/memory metrics, without a central
coordinator. That is still visible today in
[`docs/REPLICATION_STRATEGIES.md`](../REPLICATION_STRATEGIES.md) and the
polymorphic engine described in
[`docs/POLYMORPHIC_SYSTEM.md`](../POLYMORPHIC_SYSTEM.md).

> This is the parent post of the whole journal. Everything after it — transports,
> content-addressing, S3, P2P, durability, mounting — is this playground
> hardening into a real distributed object store.

## The first architectural commitments

1. **Go** for the daemon (single static binary, first-class concurrency).
2. **Daemons, not a client/server split** — nodes run identical roles.
3. **`context.Context` everywhere** for shutdown, deadlines, and cancellation —
   still a project rule today (Rule set, `docs/STANDARDS.md`).
4. **Config via INI** (`gopkg.in/ini.v1`) — small, testable, declarative.

## ⚡ Bolt lens

Even the earliest code cared about hot paths: deadline amortization, pooled
buffers, and avoiding per-request allocations became the **Bolt mindset**
documented in `docs/STANDARDS.md` and traced through every performance post
([024](024-bolt-performance-engineering.md)).

## Where the story goes

- [002: replication strategies and the polymorphic engine](002-replication-strategies-polymorphic.md)
- [003: transport evolution TCP → QUIC](003-transport-tcp-to-quic.md)
- [004: content-addressed storage](004-cas-content-addressable-store.md)