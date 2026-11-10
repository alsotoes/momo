# Observability — control-plane alerts

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1303

## Purpose

Turn the exported coordination, peer-load, and adaptive-threshold metrics into
actionable alerts and dashboard panels, so their failure modes are visible before
requests degrade.

### Requirement: Coordination quorum loss is alerted

The alert set SHALL fire when a node reports any increase of
`momo_coordination_quorum_loss_total` over ten minutes, because a node that lost
its lease quorum has stopped acting as controller.

#### Scenario: A node loses quorum
- **Given** a cluster where one node loses its coordination quorum
- **When** the counter increases within ten minutes
- **Then** `MomoCoordinationQuorumLost` fires as critical after one minute

### Requirement: Coordination churn is alerted

The alert set SHALL warn when a node resigns coordination more than three times in
fifteen minutes, which indicates load or health thresholds are flapping.

#### Scenario: Repeated resignations
- **Given** a node whose health monitor keeps resigning leases
- **When** `momo_coordination_handoffs_total` increases by four in fifteen minutes
- **Then** `MomoCoordinationHandoffChurn` fires as a warning

### Requirement: Peer pressure is alerted

The alert set SHALL warn when any peer reported through gossip stays above 100
in-flight requests for five minutes, or above 90% disk utilisation for ten minutes.

#### Scenario: A peer saturates
- **Given** peer reports arriving over gossip
- **When** one peer's `momo_peer_inflight_requests` stays above 100 for five minutes
- **Then** `MomoPeerSaturation` fires as a warning

#### Scenario: A peer approaches the shedding high-water mark
- **Given** `[rebalance] disk_high_water` is the default 85
- **When** a peer's `momo_peer_disk_used_percent` stays above 90 for ten minutes
- **Then** `MomoPeerDiskHigh` fires as a warning

### Requirement: The signals are visible on the overview dashboard

The overview dashboard SHALL graph coordination handoffs and quorum loss, per-peer
disk utilisation, per-peer in-flight requests, and the learned adaptive thresholds.

#### Scenario: Operator opens the dashboard
- **Given** Prometheus scrapes at least one node
- **When** the operator opens the Momo overview dashboard
- **Then** the four control-plane panels render with per-instance and per-peer legends
