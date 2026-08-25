# Change: Production-readiness roadmap — prioritized hardening track

**Related Issues:**
- https://github.com/alsotoes/momo/issues/928 (parent roadmap issue)
- sub-issues: R1 #929, R2 #930, R3 #931, R4 #932, R5 #933, R6 #934, R7 #935,
  R8 #936, R9 #937, R10 #938, R11 #939

## Why

Momo today is a replicated, content-addressed **blob store** with an S3-compatible
gateway and P2P gossip membership. It is not yet a **production-ready distributed
**filesystem**: the FUSE/POSIX layer (`docs/momofs/`) is design-only, metadata is
per-node bbolt (no distributed catalog/consensus), and several correctness and
operability guarantees are missing. A documentation audit (`docs/*.md`, PR #927)
confirmed the gap surface. This change ratifies the **prioritized production-readiness
roadmap** and gates future work.

## Scope (out of scope)

This change defines and ratifies the roadmap and its phase gates. It does **not**
implement any item. Each prioritized item is tracked by its own OpenSpec change
(under `openspec/changes/`) and GitHub issue, referencing this change.

## Phases & Items

### Phase P0 — correctness & durability (blockers, implement first)

| ID | Item | Deliverable | Status |
|----|------|-------------|--------|
| R1 | Failure-domain-aware CRUSH placement | Rack/zone/DC groups constrain replica placement | Planned |
| R2 | Degraded-read + self-heal rebuild | Re-replicate after corruption/quarantine/underr replication | Planned |
| R3 | Write durability + ack quorum + defined consistency | fsync-ack semantics, survivor-set quorum, read-your-writes | Planned |
| R4 | momofs FUSE/POSIX layer | Mountable POSIX filesystem with correct metadata semantics | Planned |

### Phase P1 — operability, multi-tenancy, security

| ID | Item | Deliverable | Status |
|----|------|-------------|--------|
| R5 | Metrics phases 2–4 + dashboards/alerts | Storage/CAS + P2P/cluster gauges, latency histograms, alerts | Planned |
| R6 | Metadata catalog HA/consensus + backup/recovery | Distributed metadata; snapshot/restore story | Planned |
| R7 | Error model & ops | Dedicated exit codes, ENOSPC surfacing, cluster health/status | Planned |
| R8 | Multi-tenancy + authorization + audit | Identity/ACL/policy, audit logging | Planned |
| R9 | Secrets management + key rotation | Env/KMS secrets, rotation for master/tenant/OPRF/auth keys | Planned |

### Phase P2 — S3 breadth & scale

| ID | Item | Deliverable | Status |
|----|------|-------------|--------|
| R10 | S3 lifecycle/versioning/notification/lock breadth | Extends #820 long tail; real-tooling compatibility | Planned |
| R11 | Auto-rebalance on membership change | Dynamic data movement on node join/leave | Planned |

## Definitions of Done per Phase

- **P0**: each sub-change merged with tests (`-race`, goleak), docs updated (Rule 27),
  CI green; momo passes POSIX smoke + kill-node rebuild assertions.
- **P1**: each sub-change merged; `make test` green; observability/ops docs updated.
- **P2**: each sub-change merged; S3 conformance & membership e2e green.
