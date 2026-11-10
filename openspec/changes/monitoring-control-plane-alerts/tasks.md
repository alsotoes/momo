# Tasks — control-plane alerts

## Alerts
- [x] `MomoCoordinationQuorumLost` — `increase(momo_coordination_quorum_loss_total[10m]) > 0`, 1m, critical
- [x] `MomoCoordinationHandoffChurn` — `increase(momo_coordination_handoffs_total[15m]) > 3`, 5m, warning
- [x] `MomoPeerSaturation` — `max by (instance) (momo_peer_inflight_requests) > 100`, 5m, warning
- [x] `MomoPeerDiskHigh` — `max by (instance) (momo_peer_disk_used_percent) > 90`, 10m, warning

## Dashboard
- [x] Panel: coordination handoffs + quorum loss (two series)
- [x] Panel: per-peer disk utilisation
- [x] Panel: per-peer in-flight requests
- [x] Panel: learned adaptive upper/lower thresholds
- [x] Dashboard `version` bumped; JSON validated

## Documentation
- [x] Testing guide alert table and dashboard inventory (25 panels)

## Verification
- [x] `alerts.yml` parses as YAML with 14 rules
- [x] Dashboard JSON parses with 25 panels
- [x] Rule 76: no blog post (see ADR justification); `make adr-sync-check`, `make blog-check`
