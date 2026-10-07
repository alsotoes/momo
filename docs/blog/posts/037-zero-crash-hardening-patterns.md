---
title: "🛡 Zero-Crash Hardening: Defensive Patterns for a Networked Object Store"
date: 2026-06-03T06:03:39Z
draft: false
post_type: issue
tags: [security, robustness, sentinel, bolt]
categories: [governance]
summary: "Systematic defensive coding: nil-safety, numeric overflow guards, resource lifecycle discipline, panic recovery at every boundary, and concurrency safety under -race."
artifacts:
  - {type: spec, path: openspec/changes/zero-crash-hardening}
  - {type: issue, id: "135"}
related:
  - 015-sentinel-security-audit
  - 004-cas-content-addressable-store
---
A networked object store lives at the mercy of its inputs. Every byte that arrives
over a socket is attacker-controlled until proven otherwise, and a single malformed
packet should never be able to take a node offline. We learned this the hard way:
early on, a panic on an unexpected nil value would unwind the whole process, and a
crashed storage node means unavailable data for everyone pointed at it. Availability
*is* the feature.

This post documents the defensive patterns we applied across the codebase to make
crashes on bad input, silent corruption, and resource leaks structurally difficult
rather than merely unlikely.

## The problem

Production services face a recurring set of failure modes, and they compound:

- A panic on a nil or unexpected value takes down the whole process, not just the
  request that triggered it.
- An unchecked numeric conversion silently wraps or truncates, turning a large
  count into a small one and corrupting data without any error.
- A goroutine or connection that is not released on an error path leaks a little
  each time, until the process runs out of descriptors.
- Two goroutines touching shared state without synchronization race, and the
  result depends on timing — the hardest class of bug to reproduce.

None of these are exotic. They are the ordinary ways networked Go services fail.

## The patterns

### 1. Nil-safety

We check explicitly for nil before dereferencing anything that came from outside
the process or from a decoder. We also defensively copy slices and maps at
ownership boundaries — when a buffer is handed from one subsystem to another, the
receiver gets its own copy rather than a view that the sender might later mutate.
An aliased slice that the owner reuses is a data race waiting to happen.

### 2. Numeric safety

Every narrowing conversion (a larger integer type into a smaller one, or a byte
into a rune) is checked for overflow before it happens. Float comparisons use an
epsilon tolerance rather than `==`, because floating-point equality is almost
never what the author means. And allocations that take a length from untrusted
input are validated first — `make([]T, n)` with an attacker-supplied `n` is an
out-of-memory bomb, so `n` is bounded before it is used.

### 3. Resource lifecycle

Cleanup is deferred so it runs on every return path, including error returns, and
we are careful that a `defer` inside a loop does not accumulate until the function
exits. Every connection and socket is released even when a panic unwinds the
stack, which is where leaks most often hide: the happy path is well tested, the
panic path is not.

### 4. Panic recovery at boundaries

Every goroutine and every transport boundary installs a small recovery shim. A
panic is converted into an error at the edge, so a malformed request returns a
failure to the caller instead of killing the daemon:

```go
defer func() {
    if r := recover(); r != nil {
        log.Printf("CRITICAL: recovered from panic: %v", r)
        *errno = syscall.EIO
    }
}()
```

The rule is simple: panics become errors, never process death. We put the recovery
at the boundary — the point where untrusted input enters and where a goroutine is
spawned — rather than scattering it everywhere, so normal code stays readable and
still fails fast during development.

### 5. Concurrency safety

Shared mutable state is guarded by a mutex; hot counters use atomic operations
instead, to avoid lock contention on the paths that run constantly. The whole test
suite runs under Go's race detector, and "clean under `-race`" is a release
condition, not a nice-to-have.

## Where it applies

The patterns are not specific to one package — they are applied everywhere input
crosses a boundary:

| Layer | Focus |
|-------|-------|
| Transport | Wire parsing, handshake, connection lifecycle |
| Storage | Metadata access, blob I/O, garbage collection, scrubbing |
| Peer-to-peer | Gossip, failure detection, peer map, scatter-gather |
| Server | Daemon loop, replication, query handlers |
| Common | Config parsing, placement, hashing |

## How we verified

Defensive code is easy to write and hard to trust, so we measured it:

- `go test -race ./...` reports zero data races.
- Panic-injection tests deliberately trigger panics at transport and storage
  boundaries and assert the process survives.
- A goroutine-leak check verifies that no test leaves background goroutines
  running.
- Chaos runs kill nodes, partition the network, and inject disk errors, and assert
  no crashes and no data corruption.

## When NOT to use this

Panic recovery at a boundary is a safety net, not a license to ignore errors.
Swallowing a panic deep inside logic hides bugs; recovering at the edge and
returning a truthful error is the point. Likewise, not every allocation needs a
pre-check — bound the ones whose size comes from untrusted input, and leave
internal, small, fixed allocations alone. Defensive copying has a cost; apply it
at ownership boundaries, not on every value.

## Standards

Per [docs/CORE/STANDARDS.md](../../STANDARDS.md): 🛡 **Sentinel** (fail-closed,
panic-to-error, no silent corruption), ⚡ **Bolt** (bounded allocations, zero-copy
defensive patterns).

## References / Dig deeper

- Spec: `openspec/changes/zero-crash-hardening`.
- Tracking issue: [#135](https://github.com/alsotoes/momo/issues/135).
- Sibling posts: [004: The Content-Addressable Store](004-cas-content-addressable-store.md),
  [015: The Sentinel Sweep](015-sentinel-security-audit.md).
