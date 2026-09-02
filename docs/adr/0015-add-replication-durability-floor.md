# 0015-add-replication-durability-floor

## Status
Accepted

## Confidence
High

## Context


## Decision
- Configurable Minimum Durability Factor: The global configuration SHALL expose a `minimum_durability_factor` setting (default `0`, meaning disabled). When `N >= 1`, the metrics controller SHALL NOT select an active mode whose achievable replica count is below `N`.
- Achievable Replica Count Computation: The controller SHALL compute a mode's achievable replica count as `min(replication_factor, number_of_daemons)` for replicated modes (`ReplicationChain`, `ReplicationSplay`, `ReplicationPrimarySplay`) and `1` for `ReplicationNone`.
- Degrade is Held at the Floor: In `GetMetrics`, when `checkMetricsAndSwap` or the timeout fallback proposes a mode with achievable replica count below the configured minimum, the controller SHALL NOT switch to it and SHALL log the refusal so the operation is not silent (Rule 9).
- Operator Control Channel Unaffected: The floor SHALL apply only to the automatic metrics-driven degrade path, not to operator-driven mode changes via the change-replication control channel.
- Configuration Validation: Configuration parsing SHALL reject an invalid `minimum_durability_factor` (negative, or greater than the configured `replication_factor` when the floor is enabled) with `EINVAL`.

## Consequences


## Alternatives Considered
None documented.

## Confidence
High

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/add-replication-durability-floor/
- Blog: docs/blog/posts/...md
