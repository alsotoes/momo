# Alert on the coordination, peer-load, and adaptive signals

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1303

## Why

The daemon exports control-plane metrics introduced by the R5/R6/coordination work
(`momo_coordination_handoffs_total`, `momo_coordination_quorum_loss_total`,
`momo_peer_disk_used_percent{peer}`, `momo_peer_inflight_requests{peer}`,
`momo_adaptive_max_threshold`, `momo_adaptive_min_threshold`), but the shipped
monitoring assets stop at the original signal set. An operator has no alert for a
node losing coordination quorum, a handoff storm, or a peer sliding towards the
rebalance shedding high-water mark — the failure is noticed only when requests
degrade.

## What Changes

- Four Prometheus alerts covering quorum loss (critical), handoff churn, peer
  saturation and peer disk pressure (warnings).
- Four overview-dashboard panels so the same signals can be read directly.
- The monitoring inventory in the testing guide is updated to match.

No daemon, config, or wire change: this only consumes metrics that already exist.
