---
title: "External S3 Client Replication Downgrade: When aws-cli Can't Fan Out"
date: 2026-07-01T23:36:39Z
draft: false
tags: [s3, replication, bolt, sentinel]
categories: [s3]
summary: "External S3 clients like aws-cli don't send momo headers — the server detects this and downgrades client-side replication modes to server-side alternatives, ensuring replication never silently drops."
artifacts:
  - {type: spec, path: openspec/changes/add-external-client-replication}
  - {type: issue, id: "258"}
related:
  - 016-p2p-gossip-swim
  - 018-adaptive-scaling-peer-quality
  - 041-architecture-decision-records
---
External S3 clients (aws-cli, rclone, boto3, etc.) don't speak momo's private handshake protocol — they don't send `X-Momo-Requested-Mode` or `X-Momo-Timestamp` headers. The server previously treated missing headers as forwarded peer connections (valid timestamp ≠ `DummyEpoch`) and applied `ReplicationNone` — **no replication occurred**. Even worse, `primary-splay` (mode 3) requires the *client* to fan out to replicas, which external S3 clients fundamentally cannot do.

## The Problem

When an aws-cli `PUT` hits a momo node running in `primary-splay` mode:

1. No `X-Momo-Requested-Mode` header → server can't identify momo client
2. Timestamp parsing succeeds (uses `X-Amz-Date`) but isn't `DummyEpoch` → server thinks it's a peer forward
3. Server applies `ReplicationNone` → **single copy, silent data loss risk**
4. Even if mode respected, `primary-splay` requires client-side fan-out → aws-cli can't do it

## The Solution: Configurable Downgrade

Added `client_side_replication_modes` config (default `3` = `primary-splay`):

```ini
# momo.conf
client_side_replication_modes = 3
```

When an external S3 client connects:
1. **Detection**: Missing `X-Momo-Requested-Mode` → force `timestamp = DummyEpoch`
2. **Downgrade**: Walk `replication_order` forward to next server-side mode (e.g., `3` → `2` = `splay`)
3. **Per-transaction only**: Global polymorphic state unchanged; momo CLI still uses mode 3
4. **Configurable**: Future client-side modes added via config only — no code changes

## Implementation

- **Detection**: `s3_communicator.go` checks for `X-Momo-Requested-Mode` header absence
- **Downgrade logic**: `server.go` walks `replication_order` past client-side modes
- **Config parsing**: `config.go` uses zero-alloc CSV parser (same as `replication_order`)
- **Zero global mutation**: Per-transaction downgrade; global state preserved

## Verification

- aws-cli `PUT` to `primary-splay` node → downgrades to `splay`, replicates correctly
- momo CLI with `DummyEpoch` → uses `primary-splay` unchanged
- Concurrent momo CLI + aws-cli → each gets correct mode; global state = 3
- Config validation: `client_side_replication_modes` defaults to `3`

## Standards

Per [docs/STANDARDS.md](../../STANDARDS.md), this follows ⚡ **Bolt** (zero-alloc CSV parsing, single config key) and 🛡 **Sentinel** (fail-closed: missing header = external client, never silent `ReplicationNone`).

## Follow-ups

- Per-tenant override for `client_side_replication_modes` (future)
- Metrics counter for downgrade occurrences (Phase 3 metrics)
- Docs: `EXTERNAL_CLIENT_REPLICATION.md`, `CONFIGURATION.md`, `PROTOCOL.md`

## Artifacts

- Spec: `openspec/changes/add-external-client-replication/`
- Issue: #258
- PR: #... (merged)
