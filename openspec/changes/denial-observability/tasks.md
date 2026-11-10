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

## Docs & Compliance

- [x] `docs/REFERENCE/ARCHITECTURE.md` metric table
- [x] `docs/GUIDES/CONFIGURATION.md` audit outcomes note
- [x] OpenSpec change authored (Rule 73)
- [x] ADR generated via `make adr-sync` (Rules 77/78)
- [x] Blog post (Rule 76)
