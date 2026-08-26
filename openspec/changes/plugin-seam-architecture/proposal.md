# Change: plugin-seam-architecture — seam-over-plugin pattern for adaptive FS + mutating behaviors

**Related Issues:**
- https://github.com/alsotoes/momo/issues/946
- https://github.com/alsotoes/momo/issues/932 (R4 momofs, FS surface)
- https://github.com/alsotoes/momo/issues/930 (R2 self-heal/rebuild)

## Why

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

## What

Codify **seam-over-plugin** as the standard for adaptive/mutating behavior:

1. **Rule 74** in `openspec/config.yaml`: adaptive/mutating behavior MUST be a
   compile-time Go interface seam (in-process, injected, registry-selected via
   declarative policy). External dynamic plugins are forbidden in the data/compute
   path; allowed ONLY as read-only policy/control-plane feeds. Fast paths stay
   concrete/zero-indirect; seams dispatch at decision points only. Core trust
   invariants (CAS, content-hash, CRUSH, verify-on-read, validate→write
   chokepoint) remain in the auditable core.
2. **docs/momofs/PLUGIN_ARCHITECTURE.md**: design doc grounding the pattern,
   with a seam table (`ReadPlanner`, `RebuildConverger`, FS adaptor,
   `ReplicationStrategy`), a registry + `atomic.Pointer` declarative-policy
   mechanism, perf discipline, and the security contract. Ties to
   `docs/momofs/ADAPTIVE_SYSTEMS.md` section 12 "Pluggable Everything".
3. **OpenSpec change set** (this) for Rule 11 traceability.

## Goals / Non-Goals

- **Goals:** make "how to add an adaptive/mutating behavior" unambiguous;
  forbid dynamic plugins in the data path; keep fast paths concrete; keep core
  trust invariants auditable; document perf + security contracts.
- **Non-Goals:** implementing `ReadPlanner` / `RebuildConverger` (R2) or the FS
  adaptor (R4). No runtime plugin loader. No change to existing interfaces.

## Success Criteria

- Rule 74 present in `openspec/config.yaml`.
- `docs/momofs/PLUGIN_ARCHITECTURE.md` written and internally consistent.
- PR ships this OpenSpec change set + `Resolves #946`; `make test` green.
- Three-dot diff (`git diff master...HEAD --name-only`) contains only config.yaml,
  the new doc, and this OpenSpec set.
