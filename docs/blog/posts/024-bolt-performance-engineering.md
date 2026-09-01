---
title: '⚡ Bolt: Zero-Allocation and Hot-Path Engineering'
date: 2026-08-12 11:40:41+00:00
draft: false
tags:
- go
- bolt
- performance
- zero-alloc
- benchmarking
categories:
- performance
summary: 'The Bolt mindset codified: no heap escapes in hashing/encoding, deadline
  amortization cuts SetDeadline ~98%, allocation hotspots hunted by profile.'
artifacts:
- type: pr
  id: '795'
- type: doc
  path: docs/STANDARDS.md
- type: spec
  path: openspec/changes/perf-profiling-baseline
related:
- bolt-s3-copyresult-time-alloc
- 003-transport-tcp-to-quic
- 025-benchmark-benchstat-gate
- 007-at-rest-integrity-and-gc
- 005-crush-placement
- 026-metrics-observability
---
⚡ Bolt: Zero-Allocation and Hot-Path Engineering

`docs/STANDARDS.md` codifies the two mindsets; this post is the ⚡ side.
Bolt = measured, profiled, allocation-light engineering of hot paths.

## Core patterns

- **Zero-escape SHA-256 / hex encoding** — hashing+encoding use stack-allocated
  buffers; the CAS write chokepoint ([004](004-cas-content-addressable-store.md))
  never pressures GC per object.
- **Deadline amortization** — `SetDeadline` syscalls cut ~98% on admission
  paths; hard, phased absolute deadlines still protect against Slowloris-style
  stall (10s handshake / 60s metadata / dynamic transfer bounds).
- **Combined metadata reads** — one bbolt view where three were used (#636–#853
  lineage), fewer write-path transactions.
- Hot allocation hunters in the S3 layer — ListParts/ListObjectsV2/DELETE/GET
  handlers de-allocated by profile (#795, #828, #836, #842, #899).

## The measurement contract

No "it's probably faster" claims: benchstat gates CI
([025](025-benchmark-benchstat-gate.md)) and `.github/data/benchmark_history.csv`
tracks SQL-backed deltas. Profiling baseline is file-based `.pprof`
(`go test -cpuprofile/-memprofile`) — **never** a networked pprof endpoint on
the unauthenticated listener (Rule 75).

## ⚡↔🛡 tradeoff posture

Bolt optimizes *inside* hot paths but never *at the expense* of invariants:
verify-on-read stays on ([007](007-at-rest-integrity-and-gc.md)), bounds stay
bounded, and the integrity/crypto/CRUSH core stays compiled-in (Rule 74).

## Related

Transport example: [003](003-transport-tcp-to-quic.md). Measurement:
[025](025-benchmark-benchstat-gate.md). Integrity: [007](007-at-rest-integrity-and-gc.md).