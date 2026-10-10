---
title: "Seams, Not Plugins: A Fast Path That Stays Concrete"
date: 2026-08-26T16:45:18Z
draft: false
post_type: architecture
tags: [architecture, performance, security, bolt, sentinel]
categories: [governance]
summary: "Adaptive behaviors use compile-time Go interface seams injected at decision points — not dynamic plugins. The fast path stays concrete; the trust core stays auditable; policy is declarative."
artifacts:
  - {type: spec, path: openspec/changes/plugin-seam-architecture}
  - {type: issue, id: "946"}
related:
  - 042-perf-profiling-baseline
  - 018-adaptive-scaling-peer-quality
  - 020-r2-degraded-read-self-heal
  - 043-reduce-read-verify-hashing
  - 053-multitenancy-authorization-audit
  - 054-secrets-management-key-rotation
  - 055-adaptive-volume-storage
---

"Should everything be a plugin?" The question arrives whenever a codebase wants
adaptive, mutating behavior — the ability to swap in a new read-verification
policy, a new replication strategy, or a new filesystem policy without a rebuild.
For momo the answer was **no, and the distinction matters**.

The vocabulary first. A **plugin** in the dynamic sense is code loaded at runtime
— a shared object, a separate process, or a remote procedure call — that the host
did not compile and cannot fully audit. A **seam** is the opposite: a small Go
interface compiled into the binary at a known decision point, where the concrete
implementation is chosen by configuration. The **fast path** is the common, happy
case that must stay lean; the **trust core** is the set of invariants (content
addressing, placement, integrity) that must never be swappable. A **policy** is
declarative data that selects behavior; **fail closed** means an unknown or absent
policy falls back to the safe default.

## The Real Problem

We wanted adaptability without paying for it in latency or trust. Two forces
pushed in opposite directions. The data path is performance-critical: an RPC hop
per decision would break the byte flow and add allocation to every transfer. And
the storage node is security-critical: loading unreviewed code onto a node that
holds other people's data is a Trojan surface, not a feature.

## Why the Obvious Solutions Failed

| | External dynamic plugins | In-process seams (chosen) |
|---|---|---|
| Loading | shared objects, subprocesses, cross-process RPC | compiled-in Go interface |
| Fast path | serialization and allocation tax | concrete, zero-indirection |
| Trust | executing unreviewed code | compile-time auditable |
| Versioning | pinning pain across processes | a single binary |

Dynamic plugins are the right answer when the extension authors are unknown and
the host must isolate them — think a browser extension or an editor. They are the
wrong answer for a storage node whose entire value proposition is predictable
performance and auditable behavior.

{{< diagram src="/diagrams/14-plugin-seam.svg" alt="Compile-time seam architecture" caption="Figure 1: Core storage invariants remain concrete while seams provide clean decision points" >}}

The constraint that decided it: **seam over the changeable, keep the fast path
concrete.** Seams dispatch only at decision points, never inside the byte stream.

{{< diagram src="/diagrams/14a-seam-architecture.svg" alt="Seams vs Dynamic Plugins" caption="Figure 2: In-process interface dispatch vs out-of-process dynamic RPC plugins" >}}

## The Rule

We codified the architecture as a steering rule with five commitments:

- **Adaptive, mutating behaviors are seams** — Go interfaces such as a read
  verifier, a rebuild converger, a filesystem policy, a replication strategy, and
  a durability barrier.
- **Fast paths stay concrete** — seams dispatch at decision points only, never
  per byte.
- **Trust-core invariants are pinned** — content addressing, content hashing,
  placement, verify-on-read, and the single validate-then-write chokepoint stay
  compiled into the auditable core.
- **Policy is declarative** — behavior is selected by swapping a policy struct
  through an atomic pointer at runtime, never by mutating code.
- **Fail closed** — an unknown or absent strategy falls back to the safe default
  rather than guessing.

{{< diagram src="/diagrams/14b-seam-vs-plugin.svg" alt="Seams vs Plugins Tradeoff Matrix" caption="Figure 3: Comprehensive tradeoff comparison between seams and dynamic plugins" >}}

## Examples Already in the Tree

- The **read verifier** seam — trust-earned read verification, where a blob that
  has proven itself is verified less often.
- The **durability barrier** seam — fsync, group commit, or no barrier behind one
  interface.
- The **checksum provider** seam — protocol-agnostic integrity verification, so
  every transport reports expectations the same way.

## ⚡ Bolt / 🛡 Sentinel lens

⚡ **Bolt**: concrete fast path, zero indirection, no RPC in the data plane.
🛡 **Sentinel**: fail-closed policy, no dynamic code loading, the trust core
pinned and auditable. Out-of-process code is allowed only as a read-only policy
or control-plane feed, never in the compute plane.

See [docs/CORE/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Spec: `openspec/changes/plugin-seam-architecture`.
- Design doc: `docs/momofs/PLUGIN_ARCHITECTURE.md`.
- Steering rule: `openspec/config.yaml` (Rule 74).
- Sibling posts: adaptive loops [018](018-adaptive-scaling-peer-quality.md),
  integrity seam [031](031-core-integrity-verification.md),
  read-verify seam [043](043-reduce-read-verify-hashing.md),
  performance discipline [042](042-perf-profiling-baseline.md),
  degraded-read/self-heal [020](020-r2-degraded-read-self-heal.md).
