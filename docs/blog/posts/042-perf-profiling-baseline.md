---
title: "Perf Profiling Baseline: Measure Before You Optimize — and Keep pprof Off the Wire"
date: 2026-08-26T17:46:56Z
draft: false
post_type: architecture
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
You cannot optimize what you cannot measure, and you cannot tell a real win from
a lucky benchmark run without a reference point. So before we changed a single
line of performance code, we built the measurement harness — and in the process
settled a security question that will outlive every optimization.

A **profile** is a sampled record of where a program spends its time or memory.
Go ships a profiler called **pprof** that can capture a **CPU profile** (which
functions consumed processor time) and a **memory profile** (where allocations
happened). A **baseline** is the profile you take *before* optimizing, so every
later change can be compared against it.

## The Real Problem

We had opinions about what was slow. We did not have evidence. Worse, the
obvious way to get evidence — expose the profiler over HTTP so you can pull a
live profile from a running server — is exactly the wrong move for us.

momo's data-path server has no authentication and no TLS. A live profiler
endpoint on such a server is a **remote-code-execution-class surface**: goroutine
dumps leak internal state, an attacker who can steer what gets profiled can steer
memory behavior, and the trace endpoints can crash the process. Convenience was
not worth that.

## The Solution: Profile to a File, Never to the Wire

The harness uses Go's built-in, file-based profilers — no custom tooling, no
daemons, no listening socket:

```sh
go test -cpuprofile cpu.pprof -memprofile mem.pprof -bench . ./src/storage/...
```

The benchmark segments cover the real hot paths: content hashing, local writes,
verify-on-read, and S3 spooling. The important property is that the profile is a
**file**, produced by the test process, not a listener on a server.

The security rule that came out of this is simple and absolute: profile to disk,
never to the wire.

- `go test -cpuprofile X -memprofile X -blockprofile X` writes profile files.
- No HTTP debug listener on the data path, ever.
- Any future admin endpoint would be loopback- or Unix-socket-only, enabled at
  boot, and TLS-protected the moment it leaves the loopback interface.

## What the Baseline Found

The first real profile was a surprise that saved real work. **Every cycle of
hashing CPU was already inside `sha256.blockAVX2`** — the standard library's
SHA-256 implementation already uses AVX2, the SIMD (single-instruction,
multiple-data) vector instructions modern x86-64 CPUs provide. Our naive plan had
been to "swap in a fast SIMD SHA-256." The baseline retired that idea before it
became a pull request, because there was almost nothing left to win on this
hardware.

That is the whole case for measuring first: the baseline did not just guide an
optimization, it cancelled one.

## ⚡ Bolt / 🛡 Sentinel Lens

⚡ **Bolt**: measure, then optimize. The baseline is the reference every later
performance change is judged against by the benchstat gate.
🛡 **Sentinel**: no profiler on the wire — fail closed rather than expose a
remote-code-execution-class surface for convenience.

See [docs/STANDARDS.md](../../STANDARDS.md).

## References / Dig deeper

- Mindset standards: [docs/STANDARDS.md](../../STANDARDS.md).
- The benchstat gate that consumes this baseline:
  [025](025-benchmark-benchstat-gate.md).
- Bolt engineering overview: [024](024-bolt-performance-engineering.md).
- A Bolt optimization that shipped:
  [036](036-s3-listxml-appendformat-optimization.md).
- Follow-on work: [043](043-reduce-read-verify-hashing.md),
  [044](044-plugin-seam-architecture.md).
- The profile-baseline spec: `openspec/changes/perf-profiling-baseline`;
  tracking issue [#948](https://github.com/alsotoes/momo/issues/948).
- The "no networked profiler" rule is recorded as Rule 75 in the steering rules.
