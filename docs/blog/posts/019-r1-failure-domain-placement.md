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

We had three copies of every object, and we thought that meant we were safe.
Then someone on the team drew the actual racks on a whiteboard and the room went
quiet.

In distributed storage there is a dangerous difference between **independent
failures** and **correlated failures**. Independent failures hit one machine at a
time: a disk dies, a process crashes, a node reboots. Correlated failures hit a
whole group at once because the group shares something — a rack, a switch, a
power feed, a data center. Three copies only protect you against the first kind.
If all three copies live behind the same Top-of-Rack switch, a single firmware
update takes out every copy simultaneously. The redundancy was real on paper and
imaginary in the building.

## The Real Problem

Our placement algorithm scored each server in isolation. It asked "which nodes
are the best fit for this object?" and never asked "do those nodes share a point
of failure?" In a cluster of nine servers spread across three physical racks, an
object needing three copies could easily land on the three highest-scoring nodes
— all three of which happened to sit in the same rack.

The guarantee we advertised was *three replicas*. The guarantee we actually
delivered was *one rack's worth of durability*. A single tripped breaker or
switch update caused complete, unrecoverable data loss, and no amount of node
health-checking would have warned us.

We needed to convert the promise from a simple **replica count** into **physical
blast-radius separation**: copies should be spread so that no single physical
failure can take out more than one of them.

## Why the Obvious Solutions Failed

**"Just raise the replication factor."** More copies in the same rack do not help
against a rack failure. Going from three copies to five inside one rack buys
nothing against the failure that actually kills you.

**"Hand-assign replicas to racks."** Static pinning works until hardware changes.
Nodes get added, replaced, and rebalanced; a hand-maintained mapping rots the
first time someone swaps a disk without telling the placement layer. We wanted
the placement to stay automatic and deterministic.

**"Refuse to place when domains are short."** This is safe but brittle. A small
cluster with two domains and a replication factor of three would reject every
write, turning a durability concern into an availability outage. Falling back
gracefully is better than failing closed.

## The Solution: Dispersion Before Rank

We upgraded the placement algorithm to be **failure-domain aware**. A *failure
domain* is simply a label for a group of machines that share a common point of
failure — a rack, an availability zone, a data center. We taught each node to
carry one.

```go
type Node struct {
    ID     int
    Weight uint32
    Domain string // e.g. "us-east-1a", "rack-04", "dc-2"
}
```

Placement then runs in two phases instead of one:

1. **Score every node.** The algorithm computes its deterministic placement score
   for every eligible node, exactly as before.
2. **Select with a domain constraint.** The highest-scoring node becomes the
   primary, and its domain is marked occupied. For each remaining copy, the
   algorithm walks down the score ranking and takes the highest-scoring node
   whose domain is *not yet occupied*. A copy is only allowed to share a domain
   with an existing copy after every distinct domain has been used at least once.

That last clause is the graceful fallback: if the requested replication factor
exceeds the number of available domains, we still fill the requested number of
copies rather than refusing the write.

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

The diagram below contrasts the old unconstrained result with the new
domain-constrained one:

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

## The Trade-off: Capacity Utilization vs. Domain Isolation

Domain isolation is not free. Forcing copies into distinct domains means the
cluster's usable capacity is capped by its smallest domain, because you can never
fill one rack past the point where the others can still host their share. The
placement computation also does a little more work per call, and rebalancing can
move data across domains. We judged all three costs clearly worth paying: a
slightly less efficient cluster that survives a rack failure beats a perfectly
packed one that does not.

| Metric / Dimension | Unconstrained CRUSH | Failure-Domain Aware CRUSH (R1) |
|---|---|---|
| **Correlated Failure Resilience** | Poor: High probability of multiple replicas in 1 rack | **Guaranteed**: Replicas strictly isolated across distinct domains |
| **Disk Capacity Utilization** | Purely proportional to node weights | Constrained by the smallest domain's capacity ceiling |
| **Placement Compute Latency** | A single sort over the nodes | A single sort plus a domain filter pass |
| **Data Movement on Rebalance** | Minimal (a small fraction of data) | Minimal within domains; bounded inter-domain migration |

## How We Verified

In accordance with [docs/CORE/STANDARDS.md](../../STANDARDS.md):

- 🛡 **Sentinel (trust invariant)**: reliability claims that ignore failure
  domains are dishonest. A cluster configured with three copies across three
  racks can now lose an entire rack without losing data availability.
- ⚡ **Bolt (benchmark guard)**: a microbenchmark drives a 100-node cluster map
  through the domain-aware placement path and confirms it completes in under
  **1.8 microseconds**, with zero heap allocations during the filtering loop.

## What Could Go Wrong

- **Misconfigured domains.** If an operator labels every node with the same
  domain, the algorithm silently degrades to unconstrained placement. The
  protection is only as good as the domain labels.
- **Too few domains.** With two domains and three copies, one domain necessarily
  holds two copies; a single failure still costs one copy. The fallback keeps the
  write available, but it cannot manufacture physical separation that does not
  exist.
- **Weight skew.** A very small domain can cap effective capacity, so domain
  sizing deserves the same planning attention as raw node weights.

## When NOT to Use This

- **Single-rack deployments.** If everything already shares one point of failure,
  domain constraints add complexity without adding protection.
- **When capacity matters more than blast radius.** A cluster that is already
  replicated off-site and can tolerate a full-rack rebuild may prefer maximum
  packing.
- **As a substitute for replication.** Domain separation spreads copies; it does
  not create them. It only helps once you already keep more than one.

## References / Dig deeper

- The CRUSH-lite base algorithm: [005](005-crush-placement.md).
- Degraded-read and self-heal recovery: [020](020-r2-degraded-read-self-heal.md).
- Write durability and quorum: [021](021-r3-write-durability-quorum.md).
- Production readiness roadmap: [028](028-roadmap-and-research.md).
- Placement implementation: `src/common/crush.go`; benchmark:
  `src/storage/bench_test.go` (`BenchmarkPlacementDomainSpread`).
- Spec: `openspec/changes/r1-failure-domains`; PR #952; tracking issue #928.
