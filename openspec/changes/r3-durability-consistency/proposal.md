# Change: R3 — Write durability + ack quorum + consistency model

**Related Issues:**
- https://github.com/alsotoes/momo/issues/931 (R3)
- https://github.com/alsotoes/momo/issues/928 (roadmap parent)
- https://github.com/alsotoes/momo/issues/822 (durability floor baseline)

## Why

Momo's write path has no defined durability contract. A replica set acknowledges a write
without a stated fsync-before-ack guarantee, and a write that partially reaches some
replicas (mid-write failure) leaves inconsistent replica states. There is no documented
consistency model (reads may observe stale or partial data). The existing
`minimum_durability_factor` (#822) is a durability *floor* but does not define fsync-ack
semantics, survivor-set quorum, or linearizable/read-your-writes behavior.

## What

1. **Durability ack**: a write is acknowledged only after the required number of replicas
   have durably persisted (fsync) the object. `fsync_before_ack` gates this.
2. **Survivor-set write quorum**: define the minimum replica ack count for a successful
   write (`write_quorum`), with a defined degraded path when it cannot be met.
3. **Consistency model**: document and enforce a single-object consistency model —
   **read-your-writes / sequential** for single-object ops (write then read must observe
   the write; overlapping writes are serialized per object), compatible with dispersed
   multi-object operations.

## Interaction with #822
`minimum_durability_factor` remains the controller's auto-degrade floor. R3 adds the
explicit fsync-ack + quorum semantics beneath it so the floor is meaningful.

## Out of scope

- Cross-object transactions / distributed consensus (R6, metadata catalog).
- Versioning/CAS multi-version (roadmap item 3 / separate).
