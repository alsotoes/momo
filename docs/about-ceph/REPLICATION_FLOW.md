# Ceph Rados Replication Flow (with Crimson Comparison)

## Overview

Ceph and crimson both use **CRUSH** for OSD selection and parallel replica writes, but their internal replication execution mechanisms differ significantly.

---

## Phase 1: CRUSH OSD Selection (Identical)

Both systems use CRUSH algorithm steps:

| Step | Function | Files |
|------|----------|-------|
| `choose_firstn` | Selects N OSDs from CRUSH map | `CrushWrapper.cc`, `CrushCompiler.cc` |
| `choose_indep` | Picks N-1 independent OSDs for replicas | Same as above |

**Key files (both):**
- `src/crush/CrushWrapper.cc` — CRUSH query wrapper
- `src/crush/CrushLocation.cc` (crimson) / `src/osd/PrimaryLogPG.h` (ceph) — tracks primary shard
- Primary OSD = first from `choose_firstn`; replicas = remaining N-1 from `choose_indep`

---

## Phase 2: Client Write Flow (Identical Path to Primary)

1. Client sends write to cluster
2. CRUSH selects **primary OSD**
3. Request routed to primary
4. Primary writes data to local storage
5. Primary triggers replication mechanism

**Both:** Primary OSD selected via CRUSH; `pg_whoami` / `peering_state.get_primary()` tracks which shard is primary.

---

## Phase 3: Parallel Replication Execution (Different Mechanisms)

### Ceph Replication Flow

```
Client Write → Primary OSD → new_repop() → RepGather → issue_repop() → 
pgbackend::submit_transaction() → Parallel fan-out to N-1 OSDs → 
C_OSD_RepopCommit::finish() → Replication complete
```

**Key Ceph functions:**
- `PrimaryLogPG::new_repop()` (`PrimaryLogPG.cc:11757`) — creates `RepGather` object
- `PrimaryLogPG::issue_repop()` (`PrimaryLogPG.cc:11710`) — submits transaction
- `C_OSD_RepopCommit::finish()` (`ReplicatedBackend.cc:89-97`) — callback when all ACK
- `RepGather` class (`PrimaryLogPG.h:864`) — tracks: `hoid`, `rep_tid`, `v`, `lock_manager`, callbacks

### Crimson Replication Flow

```
Client Write → Primary OSD → handle_rep_write_op() → handle_sub_write() → 
pg.log_operation() → RMWPipeline → pending_commits counter → 
try_finish_rmw() → Replication complete
```

**Key crimson functions:**
- `ECBackend::handle_rep_write_op()` (`ec_backend.cc:460`) — dispatches to `handle_sub_write()`
- `ECBackend::handle_sub_write()` (`ec_backend.cc:365`) — creates `Transaction`, calls `pg.log_operation()`
- `RMWPipeline` (`ec_backend.cc:54`) — tracks `tid_to_op_map` + `pending_commits` per shard
- `try_finish_rmw()` (`ec_backend.cc:493-494`) — called when `pending_commits == 0`
- `pg.log_operation()` — records log entries, updates commit state

**Crimson RMW Pipeline state (key fields):**
- `tid_to_op_map` — maps transaction ID to operation
- `pending_commits` — counter decremented per replica ACK
- `committed_to` — last committed epoch

---

## Comparison Table

| Aspect | Ceph | Crimson |
|--------|------|---------|
| **Replication trigger** | `issue_repop()` after primary write | `handle_rep_write_op()` after sub-write |
| **Parallel fan-out** | `submit_transaction()` → OSDs concurrently | `handle_sub_write()` → each replica OSD |
| **Completion tracking** | `RepGather` + `on_all_commit` callback | `RMWPipeline.pending_commits` counter |
| **Callback on done** | `C_OSD_RepopCommit::finish()` | `rmw_pipeline.try_finish_rmw()` |
| **Journal splay** | `journal-splay-width` config only | Not present (uses RMW pipeline) |
| **Primary selection** | CRUSH `choose_firstn` | CRUSH `choose_firstn` + `peering_state.get_primary()` |
| **State per PG** | `rep_queue`, `repop` objects | `rmw_pipeline` per PG |

---

## Resilience & Protection Analysis

### Data Protection Guarantees

| Failure Mode | Ceph | Crimson |
|--------------|------|---------|
| **Single OSD down** | Other replicas continue; stale flag set; retries on recovery | `pending_commits` decremented; `try_finish_rmw()` when all ACK |
| **Multiple OSDs down** | Remaining replicas complete; system marks missing; recovery rebuilds | Same; `pending_commits` may not reach 0 until enough ACKs |
| **Primary OSD failure** | New primary elected via peering; replication continues from new primary | Same; `is_primary()` changes; `notify_on_change()` triggers OB reload |
| **Network partition** | Quorum-based; split-brain handled by OSD map | Same; CRUSH map sync; pending ops buffered |
| **Write loss risk** | Low — writes committed when all N replicas ACK | Low — writes committed when all N replicas ACK (via `pending_commits`) |

### Performance Characteristics

| Metric | Ceph | Crimson |
|--------|------|---------|
| **Code path length** | `new_repop` → `issue_repop` → `submit_transaction` → fan-out | `handle_rep_write_op` → `handle_sub_write` → `log_operation` → RMW pipeline |
| **State overhead** | `RepGather` object per replication (nref counting) | `RMWPipeline` with `tid_to_op_map` + `pending_commits` |
| **Completion granularity** | Per-replication (one `RepGather` per write) | Per-PG (`pending_commits` counter tracks multiple in-flight writes) |
| **Lock complexity** | Lock manager in `RepGather`; `put()`/`get()` refcounting | No explicit lock manager; completion via counter |
| **Code complexity** | Moderate — well-established, ~15yr old code | Higher — RMW pipeline adds indirection (`tid_to_op_map`, `pending_commits`) |

### Which is Faster?

**Ceph likely faster for these reasons:**

1. **Simpler state**: `RepGather` per write vs `RMWPipeline` tracking multiple in-flight ops with `tid_to_op_map`
2. **Direct callback**: `on_all_commit` vs counter-based `pending_commits` that must hit 0
3. **No transaction mapping overhead**: Ceph passes object data directly; crimson encodes/decodes via `Transaction`
4. **Established path**: Ceph replication code is older and more optimized; crimson is newer

**However**, crimson's `RMWPipeline` may have advantages:

- **Batches multiple writes** under one pipeline (amortized overhead per write)
- **Simpler per-OSD logic** — no callback registration; just decrement counter
- **Potentially better throughput** for high-write workloads where batching wins

### Which has More Resilient Protection?

**Both provide equivalent data durability** — writes are not considered complete until all N replicas acknowledge. However:

- **Ceph** has more battle-tested recovery scenarios (15+ years, numerous OSD failures documented)
- **Crimson**'s `pending_commits` counter could have edge cases if `try_finish_rmw()` is called incorrectly (though `ceph_assert` guards it)
- **Crimson** may recover faster from partial failures because the pipeline state is simpler (just a counter + map)

**Verdict:** Ceph has slightly better **proven resilience** due to age and testing volume. Crimson offers **comparable protection** with different internal mechanics; the RMW pipeline could be more efficient at very high write throughput but has less real-world validation.

---

## Flow Diagram (Both Systems)

```text
Client Write
       │
       ▼
  CRUSH select ── choose_firstn ──► Primary OSD (index 0)
       │                         │
       ▼                         ▼
   Route to Primary        Write to local OSD disk
       │
       ▼
   Trigger replication
       │
   ┌────┴─────┐
   │          │
   ▼          ▼
Ceph         Crimson
RepGather    RMWPipeline
│            │
issue_repop  handle_rep_write_op()
│            │
▼            ▼
Parallel    Parallel
fan-out      fan-out
to N-1 OSDs  to N-1 OSDs
       │
       ▼
Wait for all ACKs
       │
       ▼
Completion:
  Ceph: C_OSD_RepopCommit::finish()
  Crimson: try_finish_rmw() when pending_commits==0
       │
       ▼
Replication complete
```

---

## Notes

- **No "splay" replication**: Term `splay` only in `journal-splay-width` config (journal metadata width) in both systems
- **CRUSH is shared**: Both use identical CRUSH algorithm for OSD placement; crimson reuses crush `builder.c`, `crush.c`, `hash.c`, `CrushWrapper.cc`
- **EC vs Replication**: Crimson's `ec_backend.cc` handles erasure-coded writes differently; this flow covers **replicated** pools only
- **Primary failure**: Both elect new primary via `peering_state`; replication continues from new primary

---

## References

| Component | Ceph Path | Crimson Path |
|-----------|-----------|--------------|
| CRUSH wrapper | `src/crush/CrushWrapper.cc` | `src/crimson/crush/CrushWrapper.cc` (via builder.c) |
| Primary shard | `PrimaryLogPG.h:864` (`RepGather`) | `pg.h:806` (`get_primary()`) |
| Replication trigger | `PrimaryLogPG.cc:11757` (`new_repop`) | `ec_backend.cc:460` (`handle_rep_write_op`) |
| Completion callback | `ReplicatedBackend.cc:89` (`C_OSD_RepopCommit`) | `ec_backend.cc:493` (`try_finish_rmw`) |
| State object | `RepGather` (`PrimaryLogPG.h:864`) | `RMWPipeline` (`ec_backend.cc:54`) |
| Journal width | `cls_journal.cc:319` (`splay_width`) | N/A |