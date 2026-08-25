> GitHub Issue URL: https://github.com/alsotoes/momo/issues/930

# r2-self-heal-rebuild Specification

## Purpose
Enable reads from a verified-good survivor set when the primary replica is unavailable
or corrupt, and a background self-heal loop that restores each blob to its target replica
count, respecting failure domains (R1). Reuses content-address verification.

## Terminology
- **Survivor** — a node holding a blob whose bytes verify against the object's SHA-256 key.
- **Survivor-set quorum** — the minimum number of verified survivors required to serve a
  read or trigger a rebuild; default `min(1, ...)` — configurable via `r2_degraded_*`.

## Read path

### R2-C1: Survivor read
- When the primary replica read fails (missing, or verify-on-read corruption), the read
  MUST attempt other placed replicas, verifying each survivor's bytes against the content
  hash, and serve the first verified survivor.
- Confirmed-corrupt survivors MUST be quarantined (per #924) so the rebuild loop can act.
- A read MUST fail `ENOENT`/`EIO` only when no verified survivor exists in the placement.

### R2-C2: Quarantine no longer final-deletes
- Quarantine must retain the option to mark-and-hold (not hard-delete) so the rebuild loop
  can replace the copy after re-replication; final delete occurs after a new verified
  replica lands. (Backward-compatible: hard-delete remains when no survivor exists.)

## Self-heal loop

### R2-C3: Rebuild scheduler
- Add a background loop (`StartRebuild`/`rebuildLoop`) mirroring `StartScrub`/`StartGC`
  (sync.Once, cancellable, goleak-safe), with interval from config (default 300s).
- Iterates content-address entries; for each whose live verified replica count < target,
  re-replicates from a verified survivor.

### R2-C4: Verify-before-use
- Rebuild MUST verify every source replica against the content hash before copying
  (reuse `HashReader` + verifying reader), so corrupt bytes are never propagated.

### R2-C5: Target & failure-domain-aware
- Rebuild target = current `replication_factor`. When R1 is present, repairs MUST prefer
  nodes in distinct failure domains from existing survivors.

### R2-C6: Bounded + thundering-herd safe
- Use a bounded worker pool; limit concurrent rebuilds per blob; cancel on store Close.
- Panic-recovered (Rule 37); metrics count repairs.

## Config

### R2-G1
- Add `[storage]` keys: `rebuild_interval` (default 300), `degraded_read` (bool, default
  true), and bounded worker-pool size (`rebuild_workers`, default 4).

## Tests

### R2-T1
- Survivor read: primary corrupt + verified survivor → serves survivor; no survivor → `ENOENT`.
- Quarantine mark-and-hold + post-rebuild replace.
### R2-T2
- Self-heal: kill/drop one of 3 replicas → loop restores to 3 (verified) replicas; with R1
  domains respected.
### R2-T3
- Verify-before-use: a corrupted survivor never propagates.
### R2-T4
- Goleak + `-race`; Close stops loop.
