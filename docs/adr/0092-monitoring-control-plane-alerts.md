# 0092-monitoring-control-plane-alerts

## Status
Accepted

## Confidence
High

## Context
The daemon exports control-plane metrics introduced by the R5/R6/coordination work
(`momo_coordination_handoffs_total`, `momo_coordination_quorum_loss_total`,
`momo_peer_disk_used_percent{peer}`, `momo_peer_inflight_requests{peer}`,
`momo_adaptive_max_threshold`, `momo_adaptive_min_threshold`), but the shipped
monitoring assets stop at the original signal set. An operator has no alert for a
node losing coordination quorum, a handoff storm, or a peer sliding towards the
rebalance shedding high-water mark — the failure is noticed only when requests
degrade.

## Decision
- Coordination quorum loss is alerted: The alert set SHALL fire when a node reports any increase of `momo_coordination_quorum_loss_total` over ten minutes, because a node that lost its lease quorum has stopped acting as controller.
- Coordination churn is alerted: The alert set SHALL warn when a node resigns coordination more than three times in fifteen minutes, which indicates load or health thresholds are flapping.
- Peer pressure is alerted: The alert set SHALL warn when any peer reported through gossip stays above 100 in-flight requests for five minutes, or above 90% disk utilisation for ten minutes.
- The signals are visible on the overview dashboard: The overview dashboard SHALL graph coordination handoffs and quorum loss, per-peer disk utilisation, per-peer in-flight requests, and the learned adaptive thresholds.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Planned
- **Docs**: Done
- **Blog post**: 

## References
- Issue: #1303
- PR: 
- Spec: `openspec/changes/monitoring-control-plane-alerts/`
- Blog: 

