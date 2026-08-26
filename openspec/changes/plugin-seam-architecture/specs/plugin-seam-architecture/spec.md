> GitHub Issue URL: https://github.com/alsotoes/momo/issues/946

# plugin-seam-architecture Specification

## Purpose
Codify the **seam-over-plugin** contract for adaptive and mutating behavior:
adaptive/mutating features are built as compile-time Go interface seams,
selected by a declarative policy, while core trust invariants stay in a flat,
auditable core and fast paths remain concrete and zero-indirect. External
dynamic plugins are forbidden in the data path.

## Steering Rules

### SR1: Compile-time seams only in the data path
Adaptive and mutating behavior MUST be implemented as in-process Go interface
seams — a Go `interface`, constructor/functional-option injection, and a
compiled-in registry — never as external dynamic plugins (hashicorp/go-plugin,
`buildmode=plugin` `.so`, cross-process RPC) in the data/compute path. Adding or
changing behavior means writing a new Go implementation, registering it, and
flipping the declarative policy — all code that passes `make test`.

### SR2: External plugins restricted to read-only policy feeds
An external plugin/process is allowed ONLY as a read-only policy or
control-plane feed (e.g., health/replication-factor inputs). It MUST NOT sit in
the object data path, MUST NOT execute store/write logic, and its outputs are
validated locally. Unknown/absent strategy names fail closed.

### SR3: Fast paths stay concrete and zero-indirect
The hot object path (single-replica read/write, verify-on-read, CAS) remains
concrete Coded with no per-op interface or dynamic dispatch. Seams dispatch only
at DECISION points (e.g., which replica / whether to fan out / which strategy),
never inside the byte stream. Adaptive overhead is paid only when the policy
actually changes behavior.

### SR4: Core trust invariants stay in the auditable core
CAS, content hashing, CRUSH/placement, verify-on-read, and the validate→write
chokepoint remain compiled into the core as flat, auditable code. A seam MUST
NOT bypass any of these. Mutating behaviors write through the same chokepoint as
normal writes.

### SR5: Declarative policy selects strategies
Strategy selection uses a versioned, read-mostly policy (e.g., a policy struct
swapped via `atomic.Pointer`) plus a compiled-in registry keyed by strategy
name. Behavior is changed by flipping policy, not by mutating global state at
runtime.

## Documentation (docs/momofs/PLUGIN_ARCHITECTURE.md)

Write a design doc that MUST include:

- **Positioning**: how the pattern fits the existing interface idiom (`Store`,
  `communicator`, `blobstore`, `integrity`) and ties to ADAPTIVE_SYSTEMS.md §12.
- **Two plugin kinds**: in-process seam (allowed) vs external dynamic plugin
  (forbidden in data path) with the performance/audit rationale.
- **Trust core** list (per SR4) and what stays beside, not inside, it.
- **Seam table**: `ReadPlanner`, `RebuildConverger`, FS adaptor (R4),
  `ReplicationStrategy` (existing polymorphism), each with interface sketch and
  dispatch location.
- **Registry + declarative policy** mechanism (per SR5).
- **Perf discipline** (per SR3) and **security contract** (per SR1/SR2/SR4).
- **Migration path**: R2 first (survivor `ReadPlanner` + `RebuildConverger`),
  then R4 FS adaptor; reuse `sync.Once`-guarded loop pattern from
  `StartScrub`/`StartGC`.
- **Anti-patterns**: plugin-everything indirection tax, dynamic `.so`, mutable
  global policy.

## Success Criteria

- `openspec/config.yaml` Rule 74 matches the SR1–SR5 content.
- `docs/momofs/PLUGIN_ARCHITECTURE.md` exists and covers every item above.
- This change set ships with the PR and links issue #946 via `Resolves`.
- `make test` green; three-dot diff limited to config.yaml, the doc, and this
  OpenSpec set.
