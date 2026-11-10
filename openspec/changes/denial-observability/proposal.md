# Denial Observability

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1319

## Why

The audit log records *completed* operations, so the requests an operator
investigates — the refused ones — leave no trace. An ACL denial returns `403` and
closes the connection; an over-quota write stores nothing. Neither writes an audit
entry, and no metric moves. "Nobody is using this bucket" and "every tenant is
being denied" look identical on a dashboard.

## What Changes

- `momo_authorization_denials_total` and `momo_tenant_quota_refusals_total`
  counters, incremented at the refusal point through an optional transport
  interface (`DenialReporter`), so mocks and hooks that do not implement it keep
  compiling.
- Audit entries with `outcome=denied` for both refusals, attributed to the tenant.
- Scope stays honest: native-protocol requests carry no tenant and run no ACL
  evaluation, so they produce no denial entries.

## Impact

- Affected specs: denial-observability (new)
- Affected code: `src/transport/communicator.go` (interface),
  `src/transport/s3_communicator.go` (audit + counters),
  `src/server/metrics_exporter.go` (families)
- No behavioural change for accepted requests.
