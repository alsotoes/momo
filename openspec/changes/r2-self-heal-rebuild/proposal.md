# Change: R2 — Degraded-read + self-heal rebuild

**Related Issues:**
- https://github.com/alsotoes/momo/issues/930 (R2)
- https://github.com/alsotoes/momo/issues/928 (roadmap parent)
- https://github.com/alsotoes/momo/issues/924 (integrity scrub baseline)

## Why

`storage-at-rest-integrity` (#924) added verify-on-read and a background scrub that
**quarantines** corrupt blobs. But quarantine is delete-only: a corrupt copy is removed
and later reads get `ENOENT`. There is no cross-replica healing — if one replica of an
object is corrupt/underrreplicated while others hold good bytes, momo does not regenerate
it from the survivors. A single lost node reduces `replication_factor` to `factor-1`
permanently (no background repair). Production durability requires degraded reads and a
self-healing re-replication loop.

## What

1. **Survivor-set degraded read**: when a blob's primary replica is unavailable/corrupt
   but another replica holds a verified-good copy, reads serve from an eligible survivor
   (survivor-set quorum), instead of failing.
2. **Self-heal rebuild**: a background loop (mirroring `StartScrub`/`StartGC`) detects
   underreplicated or quarantined blobs and re-replicates them from a verified-good
   survivor to the target replica count (respecting failure domains per R1).
3. Exploit content-addressing: a correct copy is any node that yields the content hash
   matching the object key; verify before using (reuse verify-on-read).

## Out of scope

- Consistency protocol changes (R3), membership rebalancing (R11).
- Rebuild scheduler tuning/administrative controls beyond basic interval config.
