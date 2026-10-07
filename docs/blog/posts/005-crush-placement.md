---
title: 'CRUSH-lite: Deterministic Weighted Placement'
date: 2026-06-30T13:09:01+00:00
draft: false
post_type: architecture
tags:
- go
- crush
- placement
- hashing
- bolt
categories:
- storage
summary: 'Weighted rendezvous hashing picks replicas deterministically and peer-equally without a central allocator — later hardened for failure domains. Teaches: content-hash-based placement, weighted rendezvous hashing, deterministic stable sort, failure domain filtering.'
artifacts:
- type: doc
  path: docs/REFERENCE/CRUSH.md
- type: pr
  id: '872'
- type: pr
  id: '873'
- type: spec
  path: openspec/changes/r1-failure-domains
related:
- 004-cas-content-addressable-store
- 019-r1-failure-domain-placement
- 024-bolt-performance-engineering
- 022-momofs-posix-core
difficulty: "intermediate"
pattern: "content-addressing"
teaches:
  - "content-hash-based placement"
  - "weighted rendezvous hashing"
  - "deterministic stable sort"
  - "failure domain filtering"
---
We started with a traditional approach: a central coordinator assigning replicas
to nodes. It worked for a three-node cluster — until it didn't.

## The Real Problem

The breaking point came during a network partition. The coordinator became a
single point of failure. When it went down, *no new objects could be placed* —
the entire cluster stopped accepting writes. Worse, when the coordinator
recovered, its view of the cluster was stale. It assigned replicas to nodes that
had already failed, quietly sending data into a hole.

We asked: **what if every node could compute placement independently, with zero
coordination?**

The insight came from the storage design itself. Because momo is
content-addressed ([004](004-cas-content-addressable-store.md)), every object
already has a SHA-256 content hash. What if that hash *is* the placement key?
That would make placement a pure function — same hash and same cluster map, same
replica set, on every node, with no network call.

Here, *placement* means deciding which nodes hold which replicas of an object,
and a *failure domain* is a group of machines likely to fail together (a rack, a
zone, a power feed). Keeping replicas in distinct failure domains is what
prevents a single rack outage from taking out every copy.

## Why Obvious Solutions Failed

**Central coordinator with heartbeat**

```go
// REJECTED: Single point of failure
type Coordinator struct {
    nodes map[string]*Node
    mu sync.Mutex
}
func (c *Coordinator) Assign(key string, n int) []Node { ... }
```

The coordinator becomes a bottleneck and a single point of failure. Heartbeats
add latency and still do not resolve split-brain.

**Consistent hashing (ketama/ring)**

```go
// REJECTED: Doesn't support weighted nodes properly
// Adding/removing nodes causes massive reshuffling
```

Standard consistent hashing treats every node as equal. Our nodes have different
capacities — disk, network, zone — so we needed *weighted* placement.

**Full RADOS CRUSH**

```go
// REJECTED: Overkill for our scale
// Hierarchical buckets, straws, recursive descent — too complex
```

Ceph's CRUSH models enormous hierarchical failure domains. We needed a *lite*
version: a flat topology, deterministic output, constant CPU per lookup.

## The Solution — Weighted Rendezvous Hashing

**Core insight:** every node computes the same replica list from the object
hash — no coordinator, no RPC, no central state. The technique is *weighted
rendezvous hashing*: score each candidate node with a hash of the object key
combined with the node's identity, multiply by the node's weight, and take the
highest scores.

### Overview

{{< diagram src="/diagrams/01-crush-placement.svg" alt="CRUSH-lite placement algorithm overview" caption="CRUSH-lite placement algorithm overview" >}}

### Detail: Scoring Algorithm

[🔍 Expand for scoring detail](/diagrams/01-crush-placement-detail.svg)

```go
// placementKey is the object's SHA-256 content hash (already computed on ingest)
func scoreCandidates(key BlobID, candidates []Node, n int) []Node {
    // 1. Score each candidate: hash(key ⊗ node.id) × weight
    type scored struct {
        node  Node
        score uint64
    }
    var ranked []scored
    for _, node := range candidates {
        if node.Weight <= 0 { continue }  // Skip zero-weight nodes
        h := hash(key ⊗ node.ID)         // Deterministic hash
        s := fold52(h) * uint64(node.Weight)
        ranked = append(ranked, scored{node: node, score: s})
    }

    // 2. Stable sort by score (descending)
    sort.SliceStable(ranked, func(i, j int) bool {
        return ranked[i].score > ranked[j].score
    })

    // 3. Top-N become replicas
    out := make([]Node, 0, n)
    for i := 0; i < n && i < len(ranked); i++ {
        out = append(out, ranked[i].node)
    }
    return out
}
```

**Key annotations:**

- `hash(key ⊗ node.ID)`: combining the content hash with the node ID makes the
  score deterministic and uniformly distributed. Every node computes the same
  number.
- `fold52()`: folds the 64-bit hash down to a 52-bit value so it fits exactly in
  a float64 mantissa, avoiding the precision loss that flipped replica order
  before this fix (#873).
- `sort.SliceStable`: when two nodes score exactly equal, a stable sort keeps
  their original order so different nodes do not disagree about the ranking
  (#872).
- `node.Weight`: capacity-aware scoring — a node with twice the weight tends to
  receive proportionally more replicas.

### Failure Domain Filter

After selecting the top N by score, we enforce failure-domain diversity so two
replicas do not land in the same rack or zone:

```go
func filterFailureDomains(replicas []Node) []Node {
    used := make(map[string]bool)
    for i, r := range replicas {
        if !used[r.FailureDomain] {
            used[r.FailureDomain] = true
            continue
        }
        // Find next-best from different domain
        for j := i + 1; j < len(candidates); j++ {
            if !used[candidates[j].FailureDomain] {
                replicas[i] = candidates[j]
                used[candidates[j].FailureDomain] = true
                break
            }
        }
    }
    return replicas
}
```

## Principle Callout

> **Pattern: Content-Hash-Based Placement**
> Use the object's content hash as the placement key. Score each node with
> `hash(key ⊗ node.id) × weight`, sort, take the top N. Zero coordination.
>
> **Applies when**: content-addressed storage, deterministic placement is
> required, node capacities are heterogeneous.
> **Doesn't apply**: mutable objects, or when placement must be controlled by
> an external policy.
>
> ```go
> // ✅ GOOD: Content-addressed, deterministic
> func place(key BlobID) []Node { return score(key, nodes) }
>
> // ❌ AVOID: Mutable objects, external placement policy
> func place(key string, policy PlacementPolicy) []Node { ... }
> ```

## Verification

| Property | Test | Result |
|----------|------|--------|
| Determinism | Same input → same output across nodes | ✅ |
| Weight proportionality | 2× weight → ~2× replicas | ✅ |
| Stability | Tied scores never flip order | ✅ (fixed #872) |
| Float precision | 52-bit mantissa, no drift | ✅ (fixed #873) |
| Failure domains | Replicas in different racks/zones | ✅ |

Production: under 100k req/s, placement adds under 50 µs of latency. Zero
coordinator overhead.

## Failure Modes

| Risk | Mitigation |
|------|------------|
| Hash collision (SHA-256) | 2^256 space — practically impossible; monitor for research breaks |
| Zero-weight nodes | Explicit skip in scoring loop |
| Tied scores | `sort.SliceStable` preserves insertion order |
| Float drift | `fold52()` limits to a 52-bit mantissa |
| Split-brain | Failure domain filter ensures diversity |

## When NOT to Use This

- **Mutable objects** — a content change means a new hash and therefore a new
  placement; use versioning instead.
- **External placement policy** — when placement must be controlled by an
  operator or scheduler outside the storage layer.
- **Non-content-addressed storage** — there is no content hash to key on.

See [docs/CORE/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Design doc: [docs/REFERENCE/CRUSH.md](../../CRUSH.md).
- Spec: `openspec/changes/r1-failure-domains`.
- Pull requests: #872 (stable sort), #873 (52-bit fold).
- Upstream: [004](004-cas-content-addressable-store.md). Downstream:
  [019](019-r1-failure-domain-placement.md),
  [024](024-bolt-performance-engineering.md).
