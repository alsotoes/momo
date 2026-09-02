---
title: "Seams, Not Plugins: A Fast Path That Stays Concrete"
date: 2026-08-26T16:45:18Z
draft: false
tags: [architecture, performance, security, bolt, sentinel]
categories: [governance]
summary: "Adaptive behaviors use compile-time Go interface seams injected at decision points — not dynamic plugins. The fast path stays concrete; the trust core stays auditable; policy is declarative."
artifacts:
  - {type: spec, path: openspec/changes/plugin-seam-architecture}
  - {type: issue, id: "946"}
related:
  - 042-perf-profiling-baseline
  - 018-adaptive-scaling-peer-quality
  - 043-reduce-read-verify-hashing
---
# Seams, Not Plugins

"Should everything be a plugin?" — the question comes up whenever a codebase
wants adaptive, mutating behaviors. For momo the answer is **no, and the
distinction matters**:

| | External dynamic plugins | In-process seams (chosen) |
|---|---|---|
| Loading | `.so`, go-plugin, cross-process RPC | Compiled-in Go interface |
| Fast path | RPC serialize/alloc tax | Concrete, zero-indirect |
| Trust | Executing unreviewed code | Compile-time auditable |
| Versioning | Pinning hell | Single binary |

The constraint that decided it: momo's data path is performance-critical and
security-critical. An RPC hop per decision kills the byte flow; loading
unreviewed code onto a zero-knowledge storage node is a Trojan surface. **Seam
over the changeable, keep the fast path concrete.**

## Rule 74

Codified in `openspec/config.yaml` Rule 74 and ratified by
`docs/momofs/PLUGIN_ARCHITECTURE.md`:

- **Adaptive/mutating behaviors** = Go interface seams (`ReadVerifier`,
  `RebuildConverger`, `FSPolicy`, `ReplicationStrategy`, `DurabilityBarrier`)
- **Fast (happy) paths stay concrete** — seams dispatch only at decision
  points, never inside the byte stream
- **Trust core invariants pinned**: CAS, content hashing, CRUSH placement,
  verify-on-read, the validate→write chokepoint stay compiled into the auditable
  core
- **Declarative policy** selects behavior — an `atomic.Pointer` to a policy
  struct, swapped at runtime, never code mutation
- **Fail closed**: unknown/absent strategy → default safe behavior

## Examples already in the tree

- `ReadVerifier` ([043](043-reduce-read-verify-hashing.md)) — trust-earned read
  verification seam
- `DurabilityBarrier` — fsync / group-commit / none behind one interface
- `ChecksumProvider` ([031](031-core-integrity-verification.md)) —
  protocol-agnostic integrity

## ⚡ Bolt / 🛡 Sentinel lens

⚡ **Bolt**: concrete fast path, zero-indirection, no RPC in the data plane.
🛡 **Sentinel**: fail-closed policy, no dynamic code loading, trust core
pinned and auditable. Out-of-process code is allowed only as a read-only
policy/control-plane *feed*, never in the compute plane.

See [docs/STANDARDS.md](../../STANDARDS.md) and
`docs/momofs/PLUGIN_ARCHITECTURE.md`.

## Related

Adaptive loops: [018](018-adaptive-scaling-peer-quality.md). Integrity seam:
[031](031-core-integrity-verification.md). Read-verify seam:
[043](043-reduce-read-verify-hashing.md). Perf discipline:
[042](042-perf-profiling-baseline.md).
