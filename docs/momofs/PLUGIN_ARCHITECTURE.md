# Plugin Architecture — Seam-Over-Plugin for Adaptive Filesystem + Mutating Behaviors

## Positioning

Momo is becoming an **adaptive filesystem** (R4 momofs, issue #932) with
**mutating behaviors** (R2 degraded-read + self-heal rebuild, issue #930). These
are whole-project capabilities, not isolated features, so *how* behavior is made
extensible matters as much as the behavior itself two bars must never slip:

- **Performance** — alloc-free fast paths, existing benchmark discipline.
- **Security** — verify-on-read, an auditable integrity model.

The extension mechanism is a **compiled, in-process Go interface seam**, chosen
by **declarative policy** — not external dynamic plugins. This document defines
that contract. It refines `docs/momofs/ADAPTIVE_SYSTEMS.md` §12 "Pluggable
Everything" into a concrete, enforceable pattern and codifies it as steering
**Rule 74** (`openspec/config.yaml`).

The project already uses the correct idiom: `Store`
(`src/storage/storage.go:68`), plus interfaces in `communicator`, `blobstore`,
and `integrity`. This document makes the *selection and boundary rules*
unambiguous so every future adaptive/mutating behavior follows the same shape.

## Two plugin kinds

| | In-process seam | External dynamic plugin |
|---|---|---|
| Form | Go `interface` + constructor injection + compiled-in registry | hashicorp/go-plugin, `buildmode=plugin` `.so`, cross-process RPC |
| Dispatch | None until a decision point | RPC boundary per call |
| Cost | ~zero on fast path | serialize + alloc + marshaling **every call** |
| Audit | Full source in repo, reviewed | Executes unreviewable/unpinned code at runtime |
| Lifecycle | Compile/link | Version pinning, process mgmt, crash isolation |
| Verdict | **Allowed** (the standard) | **Forbidden in the data path**; policy feeds only |

Dynamic plugins lose on both bars at once: per-call RPC tax kills the alloc-free
fast path, and executing code the operator cannot audit or pin breaks the
verify-on-read model — a Trojan surface. A single-binary, same-process store has
no architectural need to pay this.

## Trust core (stays flat, concrete, auditable)

These invariants remain compiled into the core so they can be read and reviewed
in one place. Adaptive/mutating behavior lives **beside** them, never bypasses
them:

- **CAS** — content-addressing and immutable objects.
- **Content hashing** — object key = content hash.
- **CRUSH / placement** — replica placement (`src/common/crush.go`).
- **Verify-on-read** — a good replica is one whose hash matches the key.
- **validate → write chokepoint** — every write, normal or mutating, passes the
  same verification and atomic-write path.

## Seam table

| Seam | Role | Dispatch point | Shape (sketch) |
|---|---|---|---|
| `ReadPlanner` | Decide fast-path vs degraded/survivor quorum read | Read request (decision, pure, no IO) | `Plan(ctx, obj, state) ReadPlan` |
| `RebuildConverger` | Background self-heal / re-replication sweep | `sync.Once`-guarded background loop | `Sweep(ctx, policy) error` |
| FS adaptor | R4 POSIX semantics over same CAS chokepoint | Mount syscall surface | `Open/Read/Write/Rename/...` |
| `ReplicationStrategy` | Existing runtime polymorphism | Metrics-driven switch (existing) | `ReplicationNone/Chain/Splay/PrimarySplay` |

The first three are *new*; the last is the existing polymorphism this pattern
extends. Both new loops mirror the house idempotence pattern of `StartScrub`
(`integrity.go:89`) and `StartGC` (`gc.go:34`) — single goroutine, work queue,
`sync.Once` guard, `context.Context` cancellation, resumable.

## Registry + declarative policy

Strategies are keyed in a **compiled-in registry**. A **versioned, read-mostly
policy** (a struct swapped via `atomic.Pointer`) selects which strategy runs at
each decision point.

```go
type ReadPlanner interface{ Plan(ctx context.Context, o Object, s State) ReadPlan }
type RebuildConverger interface{ Sweep(ctx context.Context, p Policy) error }

// registrations are compiled-in
var readPlanners = map[string]ReadPlanner{ "fast": fast{}, "degraded": degraded{} }

// policy swap is atomic; no runtime mutation of global state
var readPolicy atomic.Pointer[ReadPolicy]
```

Behavior changes = write a new `ReadPlanner`/`RebuildConverger`, register it,
flip the policy. Never mutate global state at runtime (Rule 1 / Rule 6).

## Perf discipline

- Fast path (single-replica read/write, verify-on-read) stays **concrete** —
  no per-op interface dispatch, no dynamic lookup.
- **Adaptive overhead is paid only when the policy actually changes behavior**:
  topology check → decide → maybe fan out. The happy path never pays quorum or
  indirection cost.
- Decision contexts/objects are pooled or reused where the path is hot.
- Never dispatch a seam inside the byte stream; dispatch at decision boundaries.

## Security contract

- A seam MUST NOT bypass trust-core invariants (chokepoint, verify, CRUSH).
- Mutating writes go through the same **validate → write** chokepoint as normal
  writes. Content-addressing makes correctness provable: a rebuild target is
  correct iff it yields the key hash.
- External processes feed **policy data only** (health, replication-factor
  inputs); they never execute store/write logic.
- **Fail closed**: unknown or absent strategy name → error, not a silent default
  that changes data behavior.

## Migration path

1. **R2 first** (issue #930): introduce `ReadPlanner` (survivor-set degraded read)
   + `RebuildConverger` (self-heal sweep). Smallest step that proves the whole
   pattern — decision + converge over CAS. Leave the fast path concrete.
2. **R4 next** (issue #932): FS adaptor seam over the same chokepoint. It mutates
   bytes, never replica topology.
3. Reuse `StartScrub`/`StartGC` idempotence conventions for every backer.

## Anti-patterns

- **Plugin-everything** — interface/dynamic dispatch on every type → indirection
  tax, unreadable stacks, exploding audit surface. Seam over the *changeable*,
  keep the fast path and trust core flat.
- **Dynamic `.so` in the data path** — RPC + unreviewed code = double fail.
- **Mutable global policy** — runtime-mutated selection = races and
  non-determinism. Use `atomic.Pointer` policy swap + compiled registry.
- **Seam that bypasses the chokepoint** — an adaptor that writes around
  verify-on-write reopens the integrity hole used in `storage-at-rest-integrity`.

## References

- `docs/momofs/ADAPTIVE_SYSTEMS.md` §12 — Pluggable Everything.
- `openspec/config.yaml` Rule 74 — the steering rule this doc ratifies.
- `openspec/changes/plugin-seam-architecture/` — this change set (issue #946).
- `src/storage/{storage,integrity,gc}.go` — store seam, scrub, gc loops.
