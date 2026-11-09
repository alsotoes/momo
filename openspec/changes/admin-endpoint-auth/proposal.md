# Proposal — Authenticate the Admin Listener

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1289

## Why

`StartMetricsServer` serves `/metrics`, `/health`, `/health/detailed` and
`POST /reload-secrets` with no authentication, and binds all interfaces by
default. Three of those are read-only, but `/reload-secrets` **reloads secrets and
advances the key registry** — an unauthenticated administrative mutation reachable
from the network. It has been documented as residual risk; documenting is not
fixing.

## What Changes

- New `[metrics] admin_token` (min 16 characters when set).
- When the token is set, every endpoint except `/health` requires
  `Authorization: Bearer <token>`, compared in constant time.
- When it is not set, the **mutating** endpoint is only allowed if the listener is
  bound to a loopback address; otherwise it answers `403` with an explicit
  message. Read-only endpoints keep their current behaviour for compatibility.
- `/health` always stays open: orchestrators must be able to probe liveness.

## Impact

- `src/common/struct.go` + `config.go`: `admin_token` key and validation.
- `src/server/metrics_exporter.go`: bearer gate, loopback detection, token plumbing
  through `StartMetricsServer`.
- `src/server/server.go`: pass the configured token.
- Tests: loopback allow, non-loopback deny, correct/incorrect bearer, health open,
  metrics gated, constant-time comparison path, config validation.
- Docs: CONFIGURATION (`[metrics]`), ARCHITECTURE §7b residual risk, `conf/momo.conf`.

## Alternatives

- **Reuse the data-plane `auth_token`** — couples an operator credential to every
  S3/P2P client secret. Rejected.
- **Always require a token** — breaks existing loopback-only deployments and local
  tests for no security gain (loopback is already local).
- **mTLS on the metrics listener** — strongest, but a much larger surface (cert
  management per node) than this fix needs; a bearer token is the right step now.
