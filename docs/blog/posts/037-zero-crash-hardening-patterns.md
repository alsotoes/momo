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
A networked object store can't afford to crash on malformed input, race under concurrency, or leak resources on error paths. This post documents the defensive patterns applied across the codebase.

## The Problem

Production services face:
- Panics on nil/unexpected input → whole process down
- Silent data corruption from unchecked numeric conversions
- Goroutine/connection leaks on error paths
- Data races under concurrent access

## The Patterns

### 1. Nil-Safety
- Explicit nil checks before dereferencing external/decoded data
- Defensive copying of slices/maps handed across ownership boundaries

### 2. Numeric Safety
- Overflow checks on numeric conversions (e.g., int → uint, byte → rune)
- Float comparison with epsilon, never `==`
- Bounded allocations: `make([]T, n)` validated before use (Rule 32)

### 3. Resource Lifecycle
- `defer` cleanup in loops and hot paths
- Proper close on every error return
- Connection/socket release in panic recovery (Rule 43)

### 4. Panic Recovery at Boundaries
- Every goroutine + transport boundary implements two-line recover (Rule 37):
  ```go
  defer func() {
      if r := recover(); r != nil {
          log.Printf("CRITICAL: recovered from panic: %v", r)
          *errno = syscall.EIO
      }
  }()
  ```
- Panics become errors, never process death

### 5. Concurrency Safety
- `sync.Mutex` guards shared mutable state
- `atomic` for hot counters (Rule 46)
- `-race` clean across all tests

## Where Applied

| Package | Focus |
|---------|-------|
| `transport/` | Wire parsing, handshake, connection lifecycle |
| `storage/` | Bbolt access, blob I/O, GC, scrub |
| `p2p/` | Gossip, SWIM, peer map, scatter-gather |
| `server/` | Daemon, replication, query handlers |
| `common/` | Config parsing, CRUSH, hashing |

## Verification

- `go test -race ./...` — zero data races
- Panic-injection tests on transport/storage boundaries
- `goleak.VerifyNone(t)` — no goroutine leaks
- k6 chaos: node kill, partition, disk error → no crashes

## Standards

Per [docs/STANDARDS.md](../../STANDARDS.md): 🛡 **Sentinel** (fail-closed, panic→error, no silent corruption), ⚡ **Bolt** (bounded allocations, zero-copy defensive patterns).

## Artifacts

- Spec: `openspec/changes/zero-crash-hardening/`
- Issue: #135
- PR: #... (merged)
