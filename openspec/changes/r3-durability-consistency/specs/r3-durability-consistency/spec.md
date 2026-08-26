> GitHub Issue URL: https://github.com/alsotoes/momo/issues/931

# r3-durability-consistency Specification

## Purpose
Define and enforce write durability (fsync-before-ack), a survivor-set write quorum, and
a documented single-object consistency model so that acknowledged writes are durable and
reads observe acknowledged writes.

## Terminology
- **Durable** — blob bytes flushed to stable storage (fsync) such that they survive
  process/OS crash.
- **Ack** — the moment the object store replies success to the writer.
- **Write quorum (W)** — number of durable replicas required before ack.
- **Read quorum (R)** — number of replicas consulted/verified before serving a read.

## Durability ack

### R3-C1: durability profile (fsync-before-ack)
- `[storage] durability = "fsync" | "group-commit" | "none"` selects the ack barrier.
  Invalid values are a configuration error (`syscall.EINVAL`).
- **`fsync` (default)**: a write MUST NOT be acknowledged until at least `W` replica blobs
  are durably persisted (fsync) on distinct nodes.
- **`group-commit`**: amortized batch fsync — writes are queued and a single fsync barrier
  covers a batch before any of them ack (PERFORMANCE_SECURITY B5). Still durable (each acked
  write sits on stable storage), but amortizes the per-write syscall cost for write-heavy
  deployments.
- **`none`**: acks return after target replicas have the blob buffered — best-effort,
  explicitly NON-durable (a crash may lose acknowledged writes). Must be documented as such.

### R3-C2: Write quorum
- `[global] write_quorum int` (default `1`). Valid range `1 <= write_quorum <=
  replication_factor`. If the required count cannot be reached durably, the write MUST
  fail (no silent ack), matching `minimum_durability_factor` degradation rules.

## Consistency model

### R3-C3: Sequential per-object
- For a single object, operations are serialized: a client that is acknowledged a write
  MUST observe it on subsequent reads (read-your-writes).
- Concurrent writers to the same object MUST be serialized (per-object lock or version
  fencing); the last acknowledged writer wins the object's current value (LWW preserved,
  explicitly bounded to last-ack wins).

### R3-C4: Dispersed operations
- Multi-object or namespace operations have no cross-object atomicity guarantee; each
  object obeys R3-C3 independently. This limit MUST be documented (no distributed
  transaction at this milestone).

## Interaction with #822
- The controller's auto-degrade floor (`minimum_durability_factor`) MUST NOT select a mode
  whose achievable durable replicas < `write_quorum`; otherwise the write fails.

## Config

### R3-G1
- Add `durability string` (default `"fsync"`, enum-validated) and `write_quorum int`
  (default 1) to `[storage]` / `[global]` respectively.

## Tests

### R3-T1
- Durability profile (mock store): `fsync` acks only after a durable (fsync'd) replica;
  `group-commit` acks after a batch fsync (durable, amortized); `none` acks buffered and is
  documented non-durable. Each mode exercised in isolation.
### R3-T2
- Write quorum: with W=2 and only 1 reachable/durable node, write fails (no silent ack);
  with 2 → succeeds. Never ack below minimum_durability_factor.
### R3-T3
- Read-your-writes: write → read returns new value; concurrent writer serialized (last-ack
  wins).
### R3-T4
- Goleak + `-race` across client/server/storage.
