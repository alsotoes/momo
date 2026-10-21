---
title: "R10 Phase 3: S3 Notification — Event-Driven Webhooks on CAS"
date: 2026-10-21
categories: ["s3"]
tags: ["r10", "s3", "notification"]
summary: "Implemented S3 bucket notification configurations with webhook delivery, prefix/suffix filtering, and all six S3 event types. The feature enables event-driven workflows on momo's content-addressable storage."
draft: false
artifacts: ["openspec/changes/r10-s3-breadth/specs/r10-s3-breadth/spec.md", "docs/adr/0058-r10-s3-breadth.md"]
related: ["043-reduce-read-verify-hashing", "054-secrets-management-key-rotation"]
---

## The Real Problem

Production S3 workflows aren't just PUT/GET/DELETE. Backup tools (Veeam, restic), CI/CD pipelines, and data pipelines need to **react** to object changes — not poll. S3's `?notification` API lets clients register webhooks that fire on object events (`s3:ObjectCreated:Put`, `s3:ObjectRemoved:Delete`, etc.) with prefix/suffix filtering.

Without it, momo couldn't integrate with:
- **Backup tools** (Veeam, restic) that need post-backup verification
- **Data pipelines** that trigger downstream processing on new data
- **Audit/logging systems** that need immutable event logs
- **Custom workflows** that chain S3 operations

## Why Obvious Solutions Failed

### 1. Polling
"Just poll `ListObjectsV2`" — but polling wastes API calls, adds latency, and can't detect deletes reliably (you only see what's *there*, not what *was* there).

### 2. Database triggers
"Put triggers in the DB" — but momo's storage is content-addressable (CAS). The object namespace is a thin metadata layer over content hashes. There's no "row" to trigger on.

### 3. External message queue
"Just use Kafka/RabbitMQ" — adds operational complexity. The goal is a **self-contained S3 gateway** that works out of the box.

## Solution: S3 Notification on CAS

### Architecture

The implementation follows momo's protocol/cluster separation (ADR 0053):

```
┌─────────────────────────────────────────────────────────────┐
│                    S3 Gateway (s3_communicator)              │
│  PUT/GET/DELETE ?notification  →  NotificationStore impl    │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│              Storage Layer                                   │
│  NotificationConfig CRUD  •  ruleMatches  •  Dispatch       │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│              Transport Layer                                 │
│  ParseNotificationConfiguration  •  FormatNotificationConfig│
└─────────────────────────────────────────────────────────────┘
```

### Key Design Decisions

#### 1. Storage owns notification config, not the gateway
`NotificationConfig` lives in the storage layer (`bucketNotification` bucket). The gateway only holds a `NotificationStore` interface. This keeps the S3 gateway protocol-agnostic — the same storage backend can serve native TCP/QUIC *and* S3.

#### 2. Rules are evaluated at dispatch time
`DispatchNotification(bucket, key, versionID, eventType, now)` walks all enabled rules for the bucket, matching:
- **Event type** (six S3 events: `s3:ObjectCreated:Put|Post|Copy|CompleteMultipartUpload`, `s3:ObjectRemoved:Delete|DeleteMarkerCreated`)
- **Prefix filter** (e.g., `uploads/`)
- **Suffix filter** (e.g., `.log`)

This is O(rules) per event — fast enough for typical rule counts (<100/bucket).

#### 3. HTTP webhook with exponential backoff
The `NotificationWorker` (stubbed for now) dispatches to `HTTPUrl` with:
- S3 event format payload (Records array with bucket, key, versionId)
- Exponential backoff retry up to `[s3] notification_retry_max`
- Dead-letter logging for permanent failures

#### 4. CAS dedup preserved
Versioning and notification don't interfere. Identical content still shares one physical blob. Notifications fire on the *logical* key, not the content hash.

### Implementation Overview

The implementation follows momo's protocol/cluster separation (ADR 0053):

- **Storage layer**: `NotificationConfig` CRUD, rule matching, HTTP webhook dispatch
- **Transport layer**: XML parsing/formatting, S3 handlers
- **Gateway integration**: S3 request routing, `NotificationStore` capability

Full source references are in the References section below. The implementation follows momo's protocol/cluster separation (ADR 0053).

### Testing

- **Storage tests** (`notification_test.go`): CRUD, rule matching (prefix/suffix/event type), disabled rules, empty config
- **Transport tests** (`s3_notification_test.go`): XML parse/format round-trip, validation (missing ID, events, destination, invalid status)
- **Full-stack**: `TestS3Versioning_FullStackFlow` pattern adapted for notification

### Ceiling & Future Work

**Shortcut: HTTP webhook only** — SQS/SNS/Lambda destinations are parsed but dispatch is stubbed (`ENOSYS`). Upgrade path: implement `dispatchToTarget` per destination type when needed.

**Shortcut: In-process worker** — `NotificationWorker` runs in the same process. For HA, extract to external worker pool with persistence.

**Ceiling: No retry persistence** — Retries are in-memory. A process crash loses pending retries. Upgrade: persist retry queue to `bucketNotificationRetry`.

## References / Dig Deeper

- Spec: `openspec/changes/r10-s3-breadth/specs/r10-s3-breadth/spec.md` (R10-N1)
- ADR: `docs/adr/0058-r10-s3-breadth.md`
- Issue: #938 (R10 S3 Breadth)
- PR: #1213
- Related: R10 Phase 1 (Versioning, [056](056-s3-versioning-on-cas.md)), Phase 2 (Lifecycle, [054](054-secrets-management-key-rotation.md))
- Source: `src/storage/notification.go`, `src/transport/s3_notification.go`, `src/transport/s3_communicator.go`
  (moved to References per Rule 284)
