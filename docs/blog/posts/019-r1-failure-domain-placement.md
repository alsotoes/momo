---
title: "R1: Failure-Domain-Aware Placement"
date: 2026-08-26T23:52:57Z
draft: false
post_type: architecture
tags: [go, crush, failuredomain, placement, durability, sentinel]
categories: [durability]
summary: "Replicas must not ride in the same rack: CRUSH weights become rack/zone/DC-aware so a single domain failure can't lose the data."
artifacts:
  - {type: pr, id: "952"}
  - {type: spec, path: openspec/changes/r1-failure-domains}
  - {type: issue, id: "928"}
related:
  - 005-crush-placement
  - 020-r2-degraded-read-self-heal
  - 021-r3-write-durability-quorum
---

In distributed storage, there is a dangerous difference between **independent failures** and **correlated failures**.

CRUSH-lite ([005](005-crush-placement.md)) originally scored individual nodes in isolation. In a cluster of nine servers spread across three physical server racks, an object with a replication factor of $R=3$ might have its three highest scores land on Node 0, Node 1, and Node 2. If those three nodes reside in the same physical rack, sharing a single Top-of-Rack (ToR) switch and a single Power Distribution Unit (PDU), the durability guarantee of 3 copies is an illusion: a single tripped breaker or switch firmware update causes complete, unrecoverable data loss.

Milestone **R1** (issue #928 / PR #952) upgraded momo's placement algorithm to be **failure-domain aware**, converting the guarantee from simple replica count to **physical blast-radius separation**.

```
========================================================================================
                          FAILURE-DOMAIN AWARE PLACEMENT
========================================================================================

  CRUSH-Lite Naive Placement (UNSAFE):
  +-----------------------------------------------------------------------------------+
  | Rack A (Domain: rack-1)          | Rack B (Domain: rack-2) | Rack C (Domain: rack-3) |
  | [Node 0]* [Node 1]* [Node 2]*    | [Node 3]  [Node 4]      | [Node 5]  [Node 6]      |
  +-----------------------------------------------------------------------------------+
  * All 3 replicas land in Rack A! If Rack A's PDU trips -> Complete Data Loss!

  R1 Domain-Constrained Placement (SAFE):
  +-----------------------------------------------------------------------------------+
  | Rack A (Domain: rack-1)          | Rack B (Domain: rack-2) | Rack C (Domain: rack-3) |
  | [Node 1]* (Score: 0.91)          | [Node 4]* (Score: 0.85) | [Node 5]* (Score: 0.79) |
  +-----------------------------------------------------------------------------------+
  * Replicas forced into distinct failure domains: Cluster survives any single rack loss!
```

---

## 1. The Domain Algorithm: Dispersion Before Rank

In `src/common/crush.go`, the `Node` struct was extended with an explicit `Domain` string:

```go
type Node struct {
    ID     int
    Weight uint32
    Domain string // e.g. "us-east-1a", "rack-04", "dc-2"
}
```

When `ClusterMap.Placement` calculates target nodes, it executes a two-phase selection:

1. **Calculate Scores**: Computes the deterministic 52-bit float score for every eligible node in the cluster.
2. **Domain-Constrained Selection**:
   - The node with the highest overall score is selected as primary ($N_0$). Its domain ($D_0$) is recorded as occupied.
   - For secondary replicas, the algorithm scans descending scores and selects the highest-scoring node belonging to a **yet-unoccupied domain** ($D_1 \neq D_0$).
   - If the requested replication factor $R$ exceeds the number of available physical domains, the algorithm falls back gracefully to allow duplicate domains only after every distinct domain has been populated.

```go
// src/common/crush.go
if domainsConfigured {
    selected := make([]*Node, 0, replicationFactor)
    usedDomains := make(map[string]bool)

    // Pass 1: Select highest-scoring node per unique domain
    for _, s := range scores {
        if len(selected) == replicationFactor {
            break
        }
        if !usedDomains[s.node.Domain] {
            selected = append(selected, s.node)
            usedDomains[s.node.Domain] = true
        }
    }

    // Pass 2: If R > num_domains, fill remaining slots by raw score rank
    if len(selected) < replicationFactor {
        for _, s := range scores {
            if len(selected) == replicationFactor {
                break
            }
            if !containsNode(selected, s.node) {
                selected = append(selected, s.node)
            }
        }
    }
    return selected, nil
}
```

---

## 2. Tradeoff Analysis: Capacity Utilization vs. Domain Isolation

| Metric / Dimension | Unconstrained CRUSH | Failure-Domain Aware CRUSH (R1) |
|---|---|---|
| **Correlated Failure Resilience** | Poor: High probability of multiple replicas in 1 rack | **Guaranteed**: Replicas strictly isolated across distinct domains |
| **Disk Capacity Utilization** | Purely proportional to node weights | Constrained by the smallest domain's capacity ceiling |
| **Placement Compute Latency** | Single sort ($O(N \log N)$) | Single sort + domain filtering ($O(N \log N + N)$) |
| **Data Movement on Rebalance** | Minimal ($K/N$) | Minimal within domains; bounded inter-domain migration |

---

## 3. Engineering Standards & Verification

In accordance with [docs/STANDARDS.md](../../STANDARDS.md):
- 🛡 **Sentinel (Trust Invariant)**: In distributed systems, reliability claims that ignore failure domains are dishonest. R1 guarantees that a cluster configured with $R=3$ across 3 racks can withstand a total rack destruction without data unavailability.
- ⚡ **Bolt (Benchmark Guard)**: Microbenchmarks in `src/storage/bench_test.go` (`BenchmarkPlacementDomainSpread`) verify that domain-aware evaluation completes in under **1.8 microseconds** for a 100-node cluster map, with zero heap allocations during the filtering loop.

## Related

- CRUSH-lite base algorithm: [005](005-crush-placement.md)
- Degraded-read & self-heal recovery: [020](020-r2-degraded-read-self-heal.md)
- Write durability & quorum: [021](021-r3-write-durability-quorum.md)
- Production readiness roadmap: [028](028-roadmap-and-research.md)
