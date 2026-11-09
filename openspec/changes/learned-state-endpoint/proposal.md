# Proposal — Learned State in /health/detailed

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1287

## Why

Momo adapts at runtime: it switches replication mode, learns CPU/memory baselines,
and (optionally) elects its controller by lease. All of that state is persisted
per node — but an operator has no single place to see **what the node learned**
and **what survives a restart**. Today that knowledge lives in prose
(`docs/GUIDES/CONFIGURATION.md`) or requires opening BoltDB files.

The failure mode this creates is real: an operator restarting a node cannot tell
whether the cluster will resume the mode it was running or fall back, and cannot
tell whether it is even the controller.

## What Changes

- `GET /health/detailed` gains a `learned_state` object per node:
  - `controller`: election enabled, whether this node is the lease holder, lease TTL.
  - `replication_mode`: current mode, its index in the order, the configured order,
    whether it is persisted, and the store name.
  - `adaptive`: enabled, learned upper/lower thresholds, sample count, whether it is
    persisted, and the store name.
- The block is **omitted** when no provider is installed, so nothing changes for
  embedded/test collectors.
- No absolute filesystem paths, no keys, no peer addresses are exposed.
- `docs/ARCHITECTURE.md` documents the persistence model explicitly: durable
  learned state vs ephemeral observations, and that `momo.conf` is operator-owned
  input the system never rewrites.

## Impact

- New: `src/server/learned_state.go` (pure builder), provider callback on the
  collector, `learned_state` in the detailed-health response.
- Modified: `src/metrics/adaptive.go` (expose persistence status), `src/server/server.go`
  (install the provider), `src/server/metrics_exporter.go` (field + handler).
- Tests: JSON shape, provider-nil omission, election on/off, adaptive on/off,
  no-absolute-paths assertion.
- Docs: ARCHITECTURE (new section), CONFIGURATION (cross-link).

## Alternatives Considered

- **`momo state show` CLI** — duplicates the payload behind a new parser/help
  surface and drifts from the endpoint. Rejected.
- **Write learned values back into `momo.conf`** — mixes generated state with
  operator intent, creates drift and a lost-update problem on restart. Rejected.
- **Absolute store paths in the response** — free reconnaissance on an
  unauthenticated listener. Rejected in favour of relative store names.
