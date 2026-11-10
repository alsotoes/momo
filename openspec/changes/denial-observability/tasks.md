# Tasks — Denial Observability (#1319)

## Implementation

- [x] `src/transport/communicator.go`: optional `DenialReporter` interface
- [x] `src/transport/s3_communicator.go`: ACL denial → audit `Authorize/denied` +
      `IncAuthzDenial`
- [x] `src/transport/s3_communicator.go`: `EDQUOT` → audit `PutObject/denied` +
      `IncQuotaRefusal`
- [x] `src/server/metrics_exporter.go`: `authzDenials` / `quotaRefusals` counters and
      `momo_authorization_denials_total` / `momo_tenant_quota_refusals_total`
      families (declared at zero)

## Testing

- [x] `TestS3AuthzDenialIsAuditedAndCounted` — tenant-attributed denied entry + counter
- [x] `TestS3QuotaRefusalIsAuditedAndCounted` — `403 QuotaExceeded`, denied entry, counter
- [x] `TestOnStorageErrorIgnoresNonQuotaErrors` — a generic error is not a denial
- [x] `TestQuotaRefusalMapsToForbidden` — wire contract
- [x] `TestDenialCountersAreExported` / `TestDenialCountersStartAtZero` — families and values

### Rate limiting (#1321)

- [x] `src/transport/denial_audit_limiter.go`: per-key window (1 min) + per-second
      burst cap (50), fixed-capacity insertion-ordered state
- [x] `src/transport/s3_communicator.go`: denial records go through the limiter;
      successful records unchanged
- [x] `TestDenialLimiterSuppressesRepeats`, `_ReopensAfterWindow`,
      `_CapsDistinctKeys`, `_RefillsOverTime`, `_StateIsBounded`, `_Concurrent`,
      `TestNilDenialLimiterAllows`
- [x] `TestRepeatedDenialAuditsOnce` (20 denials → 1 entry) and
      `TestSuccessfulAuditIsNeverSuppressed` (20 successes → 20 entries)

## Docs & Compliance

- [x] `docs/REFERENCE/ARCHITECTURE.md` metric table
- [x] `docs/GUIDES/CONFIGURATION.md` audit outcomes note
- [x] OpenSpec change authored (Rule 73)
- [x] ADR generated via `make adr-sync` (Rules 77/78)
- [x] Blog post (Rule 76)
