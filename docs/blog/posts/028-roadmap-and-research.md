---
title: "Forward: Production Roadmap (R5–R11) and the Research Guide"
date: 2026-08-25T03:24:12Z
draft: false
post_type: architecture
tags: [go, roadmap, research, production]
categories: [roadmap]
summary: "After R1–R4: the P0 hardening done, the P1/P2 tracks (metrics, HA metadata, auth, secrets, S3 breadth) and the research reading guide that seeded it all."
artifacts:
  - {type: issue, id: "928"}
  - {type: spec, path: openspec/changes/prod-ready-roadmap}
  - {type: doc, path: docs/ROADMAP.md}
  - {type: doc, path: docs/RESEARCH_PAPERS.md}
related:
  - 021-r3-write-durability-quorum
  - 023-momofs-fuse-transport
  - 029-fuse-go-fuse-v2-migration
---

Every system that wants to be taken seriously has to answer a boring question:
*what is left before someone can run this in production?* For a long time our
answer was a mental list. This post is the moment we wrote the list down, ordered
it, and started checking items off — and the reading list that fed the ideas in
the first place.

## Where the arc stands

{{< diagram src="/diagrams/15-roadmap-timeline.svg" alt="Momo roadmap R1-R6" caption="Momo roadmap R1-R6" >}}

The **P0 track — correctness and durability** — is done. We worked through it in
order, each rung depending on the last:

- **R1 — failure domains**: place replicas so that one rack or machine going
  dark does not take every copy of an object with it.
- **R2 — degraded reads and self-heal**: when a replica is missing, serve from
  what remains and repair the gap in the background instead of failing the read.
- **R3 — write durability and quorum**: a write is acknowledged only once enough
  replicas hold it, so an acknowledged write survives a node loss.
- **R4 — mountable POSIX filesystem**: expose the object store as a normal
  filesystem mount, first in userspace, then over a faster transport.

With the correctness floor in place, the remaining work splits into two tracks:
**P1** (operability, multi-tenancy, security) and **P2** (S3 API breadth).

| ID | Item |
|----|------|
| R5 | Metrics 2–4 + dashboards/alerts (seed: [026](026-metrics-observability.md)) |
| R6 | Metadata catalog HA + backup/recovery |
| R7 | Error model & ops (out-of-space surfacing, exit codes) |
| R8 | Multi-tenancy + authorization + audit |
| R9 | Secrets management + key rotation |
| R10 | S3 lifecycle/versioning/notification/lock breadth |
| R11 | Auto-rebalance on membership change |

Some of these deserve a gloss. **Metadata catalog HA** means the index that maps
object names to locations must survive a node loss and be restorable from
backup. **Out-of-space surfacing** means turning a raw disk-full error into a
clear, actionable signal instead of a mysterious failure. **Auto-rebalance**
means that when a node joins or leaves, the cluster moves data to restore the
placement guarantees without an operator running a script by hand.

## The research guide

None of this was invented in a vacuum. The design leans on published work:
**quorum** theory (when is a write durable?), **SWIM/gossip** (how do peers
detect failure without a central monitor?), **CRUSH/RADOS** (how do you place
data deterministically without a directory?), **private set membership** using an
oblivious pseudorandom function (how do you check membership without revealing
the query?), and filesystem semantics for the mount layer. We keep a curated
reading list so the "why" behind each mechanism is one hop away.

Our durable lens, stated once: **code is the implementation; research is the
why; specs are the contract; posts are the narrative.** Each artifact has a job,
and this journal is the one that explains rather than specifies.

## References / Dig deeper

- Roadmap: [docs/ROADMAP.md](../../ROADMAP.md).
- Research reading list: [docs/RESEARCH_PAPERS.md](../../RESEARCH_PAPERS.md).
- Spec: `openspec/changes/prod-ready-roadmap`.
- Completed P0 stack: [019](019-r1-failure-domain-placement.md),
  [020](020-r2-degraded-read-self-heal.md),
  [021](021-r3-write-durability-quorum.md),
  [022](022-momofs-posix-core.md),
  [023](023-momofs-fuse-transport.md).
