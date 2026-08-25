> GitHub Issue URL: https://github.com/alsotoes/momo/issues/941

# metrics-per-node-binding Specification

## Purpose
Allow each daemon node to opt-in to a distinct bind (`host`/`port`) for its
Prometheus `/metrics` `/health` endpoint, so `/metrics` is reliably available on
every node in same-host/co-located and hardened setups, while remaining fully
backward compatible with the global `[metrics] prometheus_port`.

## Configuration

### MPC1: Per-daemon override keys
- `[daemon.N] metrics_host` (string, optional) — bind address for this node's
  metrics server. Empty means not set (use global default).
- `[daemon.N] metrics_port` (int, optional) — bind port for this node's metrics
  server. Must be `1..65535` when present; a value outside that range is a config
  error (`EINVAL`). Empty means not set (use global default).

### MPC2: Global default bind
- `[metrics] prometheus_bind_host` (string, optional, default `""`) — bind address
  used when a node has no `metrics_host` override. Empty binds all interfaces
  (`:port`), preserving current behavior.

## Resolution & bind behavior

### MPC3: Resolve host and port
At daemon startup, for each node:
- `host` = node `metrics_host`, else global `prometheus_bind_host`, else `""`.
- `port` = node `metrics_port`, else global `prometheus_port`.

### MPC4: Bind
- `StartMetricsServer(ctx, host, port, collector)`.
- Address is `net.JoinHostPort(host, strconv.Itoa(port))`; empty host yields
  `:port` (all interfaces).
- Disabled when resolved `port <= 0`. Port-in-use logs an error (no panic) and the
  node continues (endpoint not served on that node).

### MPC5: Backward compatibility
- A config that only sets `[metrics] prometheus_port` behaves exactly as today
  (all interfaces, same port on every node — suitable for distinct-host clusters).

## Tests

### MPC-T1
Config: per-node `metrics_host`/`metrics_port` override global; fallback to global
when unset; invalid `metrics_port` (0, 65536, non-numeric) rejected.
### MPC-T2
Exporter: `StartMetricsServer` with explicit `host:port` serves `/metrics` and
`/health` (200 + body).
### MPC-T3
Same-host: two `StartMetricsServer` calls with distinct ports both bind and serve
(no EADDRINUSE).
### MPC-T4
Collision: binding an already-used port logs an error and returns without panic.
### MPC-T5
goleak + `-race` clean.
