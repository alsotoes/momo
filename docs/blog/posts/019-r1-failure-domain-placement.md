---
title: "R1: Failure-Domain-Aware Placement"
date: 2026-08-26T23:52:57Z
draft: false
post_type: architecture
tags: [go, crush, failuredomain, placement, durability]
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
CRUSH ([005](005-crush-placement.md)) chose *nodes* — but three replicas on
three nodes **in the same rack** are one power-strip away from zero copies.
R1 (#952) made placement failure-domain aware.

## The change

CRUSH-lite gains a **domain dimension** (`rack`/`zone`/`dc` groups). Placement
constrains replica selection so replicas land in *distinct* failure domains
when the topology allows:

```
rack=rack1 → weight assigns node weights on the rack
zone/DC groups push replicas apart deterministically
```

- Weights are read from the cluster config at placement time (`docs/CRUSH.md`,
  `docs/CONFIGURATION.md`).
- Same no-coordinator property as before: every node computes the same
  domain-aware order independently.

## Why it matters (Sentinel lens)

A failure domain is the *combined blast radius* — a switch, a rack PDU, a row's
cooling, a DC. `CRUSH weights` that ignore domains guarantee that the "3
replicas" durability story is **false** for exactly the correlated failures
most likely to hit. R1 upgraded the guarantee from *replica count* to *replica
separation* — the Sentinel reading of durability as a trust invariant.

## The graph position

R1 = placement constraint layer; R2 = what to do *once a replica* is lost
([020](020-r2-degraded-read-self-heal.md)); R3 = how survival is acknowledged
on write ([021](021-r3-write-durability-quorum.md)). All three are the P0
durability stack (issue #928 / `prod-ready-roadmap`).

## Related

Placement base: [005](005-crush-placement.md). Recovery: [020](020-r2-degraded-read-self-heal.md).
Durability: [021](021-r3-write-durability-quorum.md).