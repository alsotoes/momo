# Change: R1 — Failure-domain-aware CRUSH placement

**Related Issues:**
- https://github.com/alsotoes/momo/issues/929 (R1)
- https://github.com/alsotoes/momo/issues/928 (production-readiness roadmap parent)

## Why

`src/common/crush.go` uses a flat, weight-based ring for replica placement. There is
no concept of a failure domain (rack, data-center, zone). With `replication_factor=3`,
CRUSH may place all three replicas of an object on nodes that share a single point of
failure (same rack/power/network), so a single-domain outage destroys every copy.
Production durability requires copying to be spread across independent failure domains.

## What

1. Model a failure-domain hierarchy. Assign each node a failure-domain label/path
   (e.g. `rack=A`, or a hierarchy `dc=us-east1/rack=A`), configurable per `[daemon.N]`
   via a new `failure_domain` key.
2. Make `ClusterMap.Placement` failure-domain-aware: when placement selects replicas,
   prefer nodes in distinct domains. Fall back progressively (distinct domain → same
   domain) only when the cluster cannot satisfy the constraint, and log a degraded
   warning (consistent with existing degraded-mode logging).
3. Keep placement deterministic and minimal-movement when topology is stable.

## Out of scope

- Hierarchical CRUSH buckets / Ceph-style full CRUSH (design decision is flat +
  failure-domain constraint, consistent with `CRUSH-lite`).
- Live rebalancing (tracked as R11).

## References

- `docs/CRUSH.md` (placement algorithm), `src/common/crush.go`
- Roadmap REQ-1: `prod-ready-roadmap`
