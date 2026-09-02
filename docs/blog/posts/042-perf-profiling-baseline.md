---
title: "Perf Profiling Baseline: Measure Before You Optimize — and Keep pprof Off the Wire"
date: 2026-08-26T17:46:56Z
draft: false
tags: [performance, profiling, bolt, sentinel]
categories: [performance]
summary: "Phase-0 profiling harness via go test -cpuprofile/-memprofile; Rule 75 forbids networked pprof on unauthenticated listeners. The baseline proved stdlib SHA-256 already uses AVX2."
artifacts:
  - {type: spec, path: openspec/changes/perf-profiling-baseline}
  - {type: issue, id: "948"}
related:
  - 024-bolt-performance-engineering
  - 025-benchmark-benchstat-gate
  - 036-s3-listxml-appendformat-optimization
  - 043-reduce-read-verify-hashing
  - 044-plugin-seam-architecture
---
# Perf Profiling Baseline

You can't optimize what you can't measure. Phase-0 of the performance work
built the measurement harness — and proved a hard security rule along the way.

## Measure first

Before any optimization PR, momo needed a reproducible baseline. The harness
uses Go's built-in, file-based profilers — no custom tooling, no daemons:

```sh
go test -cpuprofile cpu.pprof -memprofile mem.pprof -bench . ./src/storage/...
```

Benchmark segments cover the real hot paths: content hashing, local writes,
verify-on-read, and S3 spooling. The profile is a **file**, not a listener.

## The security rule (Rule 75)

A networked `net/http/pprof` endpoint on an unauthenticated data-path server
is a **remote-code-execution-class surface**: goroutine dumps leak internals,
attacker-steerable profiling steers memory, and trace paths crash. Because momo
has no auth/TLS, the answer is simple — profile to disk, never to the wire:

- `go test -cpuprofile X -memprofile X -blockprofile X` → `.pprof` files
- No HTTP debug listener on the data path, ever
- Any future admin endpoint: loopback/Unix socket only, boot-enabled, TLS if it
  leaves loopback

## What the baseline found

The first real profile was a surprise that saved real work: **100% of hashing
CPU is inside `sha256.blockAVX2`** — the Go stdlib already ships AVX2 SIMD
assembly. The naive "swap in a SIMD SHA-256" optimization would have bought
almost nothing on amd64. Baseline-first just retired that idea before it became
a PR.

## ⚡ Bolt / 🛡 Sentinel lens

⚡ **Bolt**: measure, then optimize — the baseline is the reference every later
perf PR is judged against (the benchstat gate in [025](025-benchmark-benchstat-gate.md)).
🛡 **Sentinel**: no profiler on the wire — fail-closed over convenience.

See [docs/STANDARDS.md](../../STANDARDS.md).

## Related

Performance engineering: [024](024-bolt-performance-engineering.md). Benchstat gate:
[025](025-benchmark-benchstat-gate.md). A Bolt optimization that shipped:
[036](036-s3-listxml-appendformat-optimization.md).
