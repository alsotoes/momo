# 0027-metrics-per-node-binding

## Status
Proposed

## Confidence
Low

## Context
The Prometheus `/metrics` endpoint is started by every server process
(`server.Daemon` → `StartMetricsServer`) and thus exists on all nodes. However it
binds `:port` (all interfaces) using a single global `[metrics] prometheus_port`.
Two production gaps follow:

1. In any **same-host / co-located topology** (multi-node dev on one machine,
   host-network pods/containers sharing one IP), every node tries to bind the
   identical `:port` → `EADDRINUSE`, so only the first node's `/metrics` serves.
2. There is no way to bind `/metrics` to a specific interface or admin network —
   it is always exposed on all interfaces.

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/metrics-per-node-binding/
- Blog: docs/blog/posts/...md
