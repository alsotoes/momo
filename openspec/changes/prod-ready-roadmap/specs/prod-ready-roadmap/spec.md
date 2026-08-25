> GitHub Issue URL: https://github.com/alsotoes/momo/issues/928

# prod-ready-roadmap Specification

## Purpose
Ratify the prioritized production-readiness roadmap for momo and define the phase
gates. It is a planning/specification change: it does not implement items, it
assigns each item to an OpenSpec change + GitHub issue and defines Definition of
Done per phase.

## Terminology
- **Failure domain** — a group of nodes that share a correlated-failure risk (rack,
  DC, zone). Placement must spread replicas across distinct domains where possible.
- **Self-heal rebuild** — background re-replication that restores a blob's target
  replica count after detected corruption, quarantine, or underreplication.
- **Durability ack** — a write is acknowledged only after the required number of
  replicas have durably durable-persisted (fsync) the object.
- **Survivor-set quorum** — the minimum number of live replicas required to serve
  a read or accept a write degradation.

## Requirements

### REQ-1 (P0 gate): Failure-domain-aware placement
R1 MUST constrain CRUSH replica placement such that upstream data is spread across
distinct failure domains when the cluster topology provides them. `R1` spec: R1.

### REQ-2 (P0 gate): Degraded-read + rebuild
R2 MUST (a) allow reads to proceed from a degraded/quarantined replica set when a
survivor-set quorum is available, (b) background-regenerate blobs that are
corrupt/underrreplicated, restoring the target replica count. R2 spec: R2.

### REQ-3 (P0 gate): Write durability + consistency
R3 MUST define durability-ack semantics (fsync-before-ack), a survivor-set quorum
for writes, and a documented consistency model (sequential / read-your-writes for
single-object ops). R3 spec: R3.

### REQ-4 (P0 gate): POSIX filesystem surface
R4 MUST provide a mountable FUSE-based POSIX filesystem exposing momo's object
store with correct metadata semantics (directories, rename atomicity, hardlinks,
mmap/read-write exports, POSIX locks). R4 spec: R4.

### REQ-5 (P1): Observability & alerts
R5 MUST add storage/CAS + P2P/cluster gauges and latency histograms, plus
dashboards/alerts, building on the existing `/metrics` + `MetricsHook`.

### REQ-6 (P1): Metadata HA
R6 MUST establish a distributed metadata catalog/consensus and a documented
snapshot/backup/recovery path for metadata.

### REQ-7 (P1): Error model & ops
R7 MUST surface `ENOSPC`, add distinct fatal exit codes, and provide cluster
health/status introspection.

### REQ-8 (P1): Multi-tenancy & authorization
R8 MUST add identity, ACL/policy-based authorization, and audit logging beyond the
single shared `auth_token`.

### REQ-9 (P1): Secrets management
R9 MUST support environment/KMS secret sourcing and rotation for master, tenant,
OPRF-share, and auth-token material.

### REQ-10 (P2): S3 feature breadth
R10 MUST close high-impact S3 feature gaps (lifecycle, versioning, notification,
object lock) tracked under #820, for real-tooling compatibility.

### REQ-11 (P2): Membership rebalance
R11 MUST rebalance existing replicas when cluster membership changes.

## Phasing & Ordering
1. P0 items (R1→R4) must land, each with `-race`+goleak tests, Rule-27 docs, and
   CI green, before momo is declared production-ready for a filesystem workload.
2. P1 items (R5→R9) follow; each independently releasable.
3. P2 items (R10→R11) follow; each independently releasable.

## Non-Requirements (explicitly out of scope for this change)
- Implementing any roadmap item.
- Adding a new storage backend or metadata engine.
