# 0089-learned-state-endpoint

## Status
Proposed

## Confidence
Low

## Context
Momo adapts at runtime: it switches replication mode, learns CPU/memory baselines,
and (optionally) elects its controller by lease. All of that state is persisted
per node — but an operator has no single place to see **what the node learned**
and **what survives a restart**. Today that knowledge lives in prose
(`docs/GUIDES/CONFIGURATION.md`) or requires opening BoltDB files.

The failure mode this creates is real: an operator restarting a node cannot tell
whether the cluster will resume the mode it was running or fall back, and cannot
tell whether it is even the controller.

## Decision
- Detailed health reports learned state: `GET /health/detailed` SHALL include a `learned_state` object describing what the node learned at runtime and whether it is persisted.
- The payload never leaks infrastructure paths: `learned_state` SHALL report persistence as a boolean plus a relative store name.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1287
- PR: 
- Spec: `openspec/changes/learned-state-endpoint/`
- Blog: 

