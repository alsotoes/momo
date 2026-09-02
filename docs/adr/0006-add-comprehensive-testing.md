# 0006-add-comprehensive-testing

## Status
Accepted

## Confidence
High

## Context
While Momo has robust unit, E2E, load, and concurrency tests (via `goleak` and `-race`), it lacks advanced distributed systems testing paradigms such as failure injection, distributed load generation (e.g., k6), strict contract testing, and centralized observability (Grafana/Prometheus). This proposal outlines the steps to fill these remaining gaps to conform to production-grade distributed testing standards.

## Decision
- Jepsen-Style Network Partition Injection (Resolves #155): The testing framework SHALL support simulating network partitions (netsplit) between designated datacenter regions using standard kernel traffic control (`tc`) tools or virtual networking namespaces.
- Chaos Engineering Node Crashes (Resolves #155): The testing framework SHALL support abruptly killing random Primary or Secondary server daemons during active concurrent replication payload transfers to verify self-healing recovery.
- Distributed Load & Timeout Validation (Resolves #155): The testing framework SHALL support simulating heavy, concurrent client workloads under strict, phased timeouts and slow-network trickle (Slowloris) attacks.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/add-comprehensive-testing/
- Blog: docs/blog/posts/...md
