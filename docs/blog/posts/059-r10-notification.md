---
title: "R10 Phase 3: S3 Notification — Event-Driven Webhooks on CAS"
date: "2026-10-21T07:14:25Z"
draft: false
post_type: architecture
tags:
  - r10
  - s3
  - notification
  - webhooks
categories:
  - s3
summary: "Implemented S3 bucket notification configurations with real webhook delivery, prefix/suffix filtering, exponential-backoff retries, and all six S3 event types — so backup tools and pipelines can react to object changes instead of polling. Teaches: why a notification dispatcher that returns nil without sending is the trap to watch for, and how events stay honest across plain PUT, multipart, and delete paths."
artifacts:
  - type: spec
    path: openspec/changes/r10-s3-breadth
  - type: adr
    path: docs/adr/0058-r10-s3-breadth.md
related: ["056-s3-versioning-on-cas", "058-s3-lifecycle-on-cas", "043-reduce-read-verify-hashing", "054-secrets-management-key-rotation"]
difficulty: "intermediate"
pattern: "event-side-effects"
teaches:
  - "S3 bucket notification semantics (events, filters, webhooks)"
  - "Exponential-backoff retry with a 4xx/5xx distinction"
  - "Firing events from every write path, not just the happy one"
---

## The Real Problem

Production S3 workflows aren't just PUT/GET/DELETE. Backup tools (Veeam,
restic), CI/CD pipelines, and data pipelines need to **react** to object
changes — not poll for them. S3's `?notification` API lets a client register
webhooks that fire on object events (`s3:ObjectCreated:Put`,
`s3:ObjectRemoved:Delete`, …) with prefix/suffix filtering.

Without it, momo couldn't integrate with:

- **Backup tools** (Veeam, restic) that want post-backup verification hooks
- **Data pipelines** that trigger downstream processing on new data
- **Audit/logging systems** that want a push-based event stream
- **Custom workflows** that chain S3 operations together

## Why Obvious Solutions Failed

### 1. Polling

"Just poll `ListObjectsV2`" — polling wastes API calls, adds latency
proportional to the polling interval, and cannot detect deletes reliably: a
list shows what *is* there, never what *was* there.

### 2. Database triggers

"Put triggers in the DB" — momo's storage is content-addressable. The object
namespace is a thin metadata layer over content hashes, so there is no stable
"row" per object to hang a trigger on. Identical content is one physical blob
shared by many logical keys; the thing you want to notify about is the
*logical* write, which the CAS layer does not model.

### 3. An external message queue

"Just use Kafka/RabbitMQ" — that adds an operational dependency and a second
source of truth. The goal is a **self-contained S3 gateway** that works out of
the box; an operator who wants a queue can point a webhook at their own
gateway.

## Solution: S3 Notification on CAS

### Architecture

Notification *configuration* is storage state; the S3 gateway is only an
adapter over it. That split mirrors momo's protocol/cluster separation
(ADR 0053): the storage layer owns the rules, the transport layer owns the XML,
and the server layer decides *when* a write counts as an event.

```
S3 client ──PUT ?notification──▶ s3_communicator  ──▶ NotificationStore
                                  (XML parse/format)      (storage rules)
                                                              │
object write (PUT / DELETE / multipart complete)              │
        │                                                     │
        ▼                                                     ▼
server.afterS3ObjectWriteBucket ──────▶ DispatchNotification ─▶ HTTP POST
        (version + event)                 (rule match)          (webhook)
```

### Key design decisions

#### 1. Storage owns the configuration, not the gateway

`NotificationConfig` lives in the storage layer (a dedicated `notification`
bucket keyed by bucket name). The gateway holds only a `NotificationStore`
interface. This keeps the S3 gateway protocol-agnostic — the same storage
backend serves native TCP/QUIC *and* S3, and a store that does not implement
the capability simply answers `501 NotImplemented` (the honest-rejection
posture used across momo's S3 surface).

#### 2. Rules are evaluated at dispatch time

`DispatchNotification(bucket, key, versionID, eventType, now, retryMax)` walks
the bucket's enabled rules and matches on:

- **Event type** — six S3 events: `s3:ObjectCreated:Put|Post|Copy|CompleteMultipartUpload`
  and `s3:ObjectRemoved:Delete|DeleteMarkerCreated`
- **Prefix filter** (`uploads/`)
- **Suffix filter** (`.log`)

Every matching rule gets its own delivery attempt, and one rule failing does
not stop the others: the outcome reports both `Dispatched` and `Failed`
counts.

#### 3. Real webhook delivery with exponential backoff

This is where a naive implementation goes wrong. It is easy to write a
`dispatchToTarget` that builds a payload and returns `nil` without ever sending
it — the config CRUD tests pass and the feature looks done, but no bytes leave
the process. momo's spec (R10-N1) requires the POST to actually happen. So
delivery is a real `http.Client` call:

- The body is the S3 event document — a `Records` array carrying the bucket
  name, the object key, and the version id.
- **Network errors and 5xx** responses are retried with exponential backoff,
  up to `[s3] notification_retry_max` attempts (default 3).
- A **4xx** response returns immediately: the server has already made a
  decision about the request, and retrying a rejected request only amplifies
  load.
- The event time comes from an **injected `now`**, not `time.Now()` inside the
  dispatcher, so tests stay deterministic.

The payload is built from typed structs, not `map[string]interface{}`: the
bucket name and object key go where they belong, which sounds obvious until a
first draft puts the rule's ARNs in those fields and ships a payload that is
structurally valid and semantically wrong.

#### 4. Events fire from every write path, not just the happy one

A notification that only fires on the simplest PUT is a trap. Objects reach
storage through several paths:

- plain `PUT` and dedup-hit PUTs — recorded by the server's ingest hook
  (`afterS3ObjectWriteBucket`), which also records the version on versioned
  buckets
- `CompleteMultipartUpload` — assembled inside the gateway, so it dispatches
  its own `ObjectCreated:CompleteMultipartUpload`
- single and batch `DELETE` — dispatch `ObjectRemoved:Delete`

Native (non-S3) writes never notify: notifications are an S3-surface concept,
gated on `IsExternalClient()`.

#### 5. CAS dedup is untouched

Versioning and notification do not interfere with content addressing.
Identical content still shares one physical blob; notifications fire on the
logical key, not the content hash.

### Testing

The interesting tests are the ones that prove delivery, not just matching:

- **Storage**: delivery to a real `httptest` server with payload assertions;
  prefix/suffix/event filtering; retry-then-succeed with attempt counting;
  exhausted retries counted as `Failed`; 4xx not retried; unsupported target
  reported as a failure rather than a silent success
- **Transport**: XML parse/format round-trip, all six event mappings, invalid
  rules, every handler branch including the 501/404/400/500 paths, and the
  gateway dispatch helper's retry-budget default
- **Server**: the ingest hook end-to-end against a real `CASStore` — version
  recorded and webhook delivered — plus the native-write no-op

## Shortcuts and ceilings

- **Shortcut: HTTP webhooks only.** SQS/SNS/Lambda destinations parse and are
  rejected with an explicit `ENOSYS` at dispatch rather than silently
  succeeding. Upgrade: implement one delivery adapter per destination type.
- **Shortcut: synchronous dispatch.** Delivery runs inline in the write path,
  bounded by the retry budget, so a slow webhook adds latency to the write. An
  async worker with a persisted retry queue is the upgrade path when webhook
  latency must not touch the request path.
- **Ceiling: no retry persistence.** Retries are in-memory; a crash between
  attempts loses the pending delivery.

## References / Dig deeper

- Spec: `openspec/changes/r10-s3-breadth/specs/r10-s3-breadth/spec.md` (R10-N1)
- ADR: `docs/adr/0058-r10-s3-breadth.md`
- Issue: #938 (R10 S3 breadth)
- Related: R10 Phase 1 (Versioning, [056](056-s3-versioning-on-cas.md)), Phase 2
  (Lifecycle, [058](058-s3-lifecycle-on-cas.md))
- Source: `src/storage/notification.go`, `src/transport/s3_notification.go`,
  `src/transport/s3_communicator.go`, `src/server/server.go`
