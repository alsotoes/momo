# Tasks — Tenant Quota Enforcement (#1315)

## Implementation

### Accounting
- [x] `src/storage/quota.go`: `tenantUsage` record (`Bytes`, `Objects`), big-endian
      16-byte encoding
- [x] `src/storage/storage.go`: `tenant_usage` bucket registered in `metaBuckets`
- [x] `AdjustTenantQuota(tenantID, deltaBytes, deltaObjects)` — check + apply in one
      bbolt transaction; `ErrQuotaExceeded` wraps `syscall.EDQUOT`; negatives clamp
      at zero
- [x] `TenantUsage(tenantID)` — read side
- [x] No-op for empty tenant, unknown tenant, and both quotas unset

### Enforcement
- [x] `src/server/file.go`: reserve before `store.Put`; overwrite pays only the
      delta and never increments the object count
- [x] `src/server/file.go`: release on `store.Put` failure, hash mismatch and
      checksum mismatch
- [x] `src/server/file.go`: `tenantOf` helper shared with the audit path
- [x] `src/transport/s3_communicator.go`: `EDQUOT` → `403 QuotaExceeded`
- [x] `src/transport/s3_communicator.go`: delete releases bytes and count

### Tenant credential resolution (blocking prerequisite, #1316)
- [x] `src/transport/s3_communicator.go`: `resolveTenantToken` binds
      `m.tenantID` from a tenant `auth_token` (Bearer and SigV4 access key)
- [x] SigV4 legacy single-token mode signs with the presented tenant token
- [x] Oversized credential refused with `403` before `PadString` (was a recovered
      panic with no response)

## Testing

- [x] `TestTenantQuota_NoOpCases` — empty/unknown/unlimited tenants
- [x] `TestTenantQuota_ByteLimit` — refusal leaves usage unchanged, exact limit ok
- [x] `TestTenantQuota_ObjectLimit` — overwrite does not consume the object quota
- [x] `TestTenantQuota_ReleaseClampsAtZero`
- [x] `TestTenantQuota_ConcurrentBoundaryNeverOvershoots` — 24 writers at a 100-byte
      quota admit exactly 10
- [x] `TestGetFile_QuotaRefusedBeforeWrite` — no object, no usage
- [x] `TestGetFile_QuotaChargedOnSuccess`
- [x] `TestGetFile_QuotaOverwritePaysOnlyTheDelta`
- [x] `TestGetFile_QuotaReleaseOnHashMismatch`
- [x] `TestGetFile_NoTenantLeavesQuotasAlone`
- [x] `TestS3TenantTokenAuthenticatesAndIsDefaultDenied` (#1316)
- [x] `TestS3TenantTokenAllowedByACL` (#1316)
- [x] `TestS3UnknownTokenStillDenied`, `TestS3ClusterTokenHasNoTenant`,
      `TestS3TenantTokenResolvesAtHandshake`, `TestS3OversizedCredentialIsRefusedNotPanicked`

## Docs & Compliance

- [x] `docs/GUIDES/CONFIGURATION.md`: quotas are enforced; accounting semantics and
      the S3-only scope stated
- [x] `docs/ARCHITECTURE.md` §4l: quota accounting + tenant credential resolution
- [x] `docs/REFERENCE/ERROR_CODES.md`: `403 QuotaExceeded`
- [x] OpenSpec change authored (Rule 73)
- [x] ADR generated via `make adr-sync` (Rules 77/78)
- [x] Blog post (Rule 76)
