---
title: "Forward: Production Roadmap (R5–R11) and the Research Guide"
date: 2026-08-25T03:24:12Z
draft: false
tags: [go, roadmap, research, production]
categories: [roadmap]
summary: "After R1–R4: the P0 hardening done, the P1/P2 tracks (metrics, HA metadata, auth, secrets, S3 breadth) and the research reading guide that seeded it all."
artifacts:
  - {type: issue, id: "928"}
  - {type: spec, path: openspec/changes/prod-ready-roadmap}
  - {type: doc, path: docs/ROADMAP.md}
  - {type: doc, path: docs/RESEARCH_PAPERS.md}
related:
  - 027-governance-ai-review-spec-first
  - 021-r3-write-durability-quorum
  - 023-momofs-fuse-transport
  - 029-fuse-go-fuse-v2-migration
  - 041-architecture-decision-records
---
Issue #928 ratified the **production-readiness roadmap** (`prod-ready-roadmap`);
`docs/ROADMAP.md` gates production behind phased deliverables. This post is the
forward look — and the nod to the research that seeded the ideas.

## Where the arc stands

The **P0 correctness/durability track is done**: R1 failure domains
([019](019-r1-failure-domain-placement.md)) → R2 degraded-read + self-heal
([020](020-r2-degraded-read-self-heal.md)) → R3 write durability
([021](021-r3-write-durability-quorum.md)) → R4 mountable POSIX
([022](022-momofs-posix-core.md), [023](023-momofs-fuse-transport.md)).

Next, **P1 (operability, multi-tenancy, security)** and **P2 (S3 breadth)**:

| ID | Item |
|----|------|
| R5 | Metrics 2–4 + dashboards/alerts (seed: [026](026-metrics-observability.md)) |
| R6 | Metadata catalog HA + backup/recovery |
| R7 | Error model & ops (ENOSPC surfacing, exit codes) |
| R8 | Multi-tenancy + authorization + audit |
| R9 | Secrets management + key rotation |
| R10 | S3 lifecycle/versioning/notification/lock breadth |
| R11 | Auto-rebalance on membership change |

## The research guide

`docs/RESEARCH_PAPERS.md` is the reading list that seeded much of the design —
quorum theory, SWIM/gossip, CRUSH/RADOS, private-set-membership (OPRF), and
filesystem semantics for momofs. This journal's durable lens: **code is the
implementation; research is the why; specs are the contract; posts are the
narrative** (Rule 76 keeps the last one evergreen).

## Related

Governance that gates this: [027](027-governance-ai-review-spec-first.md). The
completed P0 stack links back through [019](019-r1-failure-domain-placement.md)
and [021](021-r3-write-durability-quorum.md).