# Change: metrics-per-node-binding — optional per-node bind (host + port) for `/metrics`

**Related Issues:**
- https://github.com/alsotoes/momo/issues/941
- https://github.com/alsotoes/momo/issues/933 (roadmap R5, related)

## Why

The Prometheus `/metrics` endpoint is started by every server process
(`server.Daemon` → `StartMetricsServer`) and thus exists on all nodes. However it
binds `:port` (all interfaces) using a single global `[metrics] prometheus_port`.
Two production gaps follow:

1. In any **same-host / co-located topology** (multi-node dev on one machine,
   host-network pods/containers sharing one IP), every node tries to bind the
   identical `:port` → `EADDRINUSE`, so only the first node's `/metrics` serves.
2. There is no way to bind `/metrics` to a specific interface or admin network —
   it is always exposed on all interfaces.

## What Changes

- Per-node opt-in override keys `[daemon.N] metrics_host` and `metrics_port`.
- Global default bind `[metrics] prometheus_bind_host`.
- `StartMetricsServer` binds `host:port` via `net.JoinHostPort`; empty host binds
  all interfaces (`:port`), preserving current behavior.
- Resolved at runtime: node override → global default → disabled when port `<= 0`.
- Backward compatible: existing configs (global-only) are unchanged.

## Non-Goals

- No cluster-aggregate `/metrics` endpoint (external Prometheus scrapes each node).
- No TLS/auth on the metrics endpoint.
- No change to the polymorphic controller (`metrics.GetMetrics`, node-0 only) —
  that is a separate concern and remains as-is.
