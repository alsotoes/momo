# 0030-plugin-seam-architecture

## Status
Accepted

## Confidence
High

## Context
Momo moves toward an **adaptive filesystem** (R4 momofs) and **mutating
behaviors** (R2 degraded-read/self-heal rebuild) — a whole-project feature set.
That direction raises the question of how to make behavior extensible while
keeping the two non-negotiable bars: **performance** (alloc-free fast paths,
existing benchmark discipline) and **security** (verify-on-read, auditable
integrity model).

The naive answer, "make everything a plugin", is dangerous here:

1. **External dynamic plugins** (hashicorp/go-plugin, `buildmode=plugin` `.so`,
   cross-process RPC) place an RPC/serialization/allocation tax on every call,
   destroying the alloc-free fast path, and execute code the operator cannot
   cheaply audit — a direct conflict with the verify-on-read/integrity model.
2. **Plugin-everything** (interface on every type) adds indirection tax,
   unreadable call stacks, and an exploding audit surface for no gain.

The project ALREADY uses the correct idiom: Go **interface seams** with
constructor injection — `Store` (`src/storage/storage.go:68`), `communicator`,
`blobstore`, `integrity` — and idempotent background loops (`StartScrub` at
`integrity.go:89`, `StartGC` at `gc.go:34`, both `sync.Once`-guarded). The
missing piece is a **codified contract** so every adaptive/mutating behavior is
built as a compile-time seam behind a declarative policy, not ad-hoc or
dynamically loaded.

## Decision


## Consequences


## Alternatives Considered
None documented.

## Confidence
High

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/plugin-seam-architecture/
- Blog: docs/blog/posts/...md
