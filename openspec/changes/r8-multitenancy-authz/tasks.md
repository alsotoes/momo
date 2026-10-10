# Tasks: R8 — Multi-Tenancy + Authorization + Audit

## Implementation

### Tenant Model + Config
- [x] `src/common/struct.go`: `TenantConfig` struct with `ID`, `MasterKeyID`, `AuthToken`, `QuotaBytes`, `QuotaObjects`, `Enabled`
- [x] `src/common/config.go`: Parse `[tenant.<id>]` sections; validate required fields
- [x] `src/storage/storage.go`: `tenant_meta` bucket; tenant CRUD methods

### Per-Tenant Key Hierarchy
- [x] `src/crypto/crypto.go`: `DeriveTenantKeys(rootKEK, tenantID) (KEK, OPRFShare, AuthToken)`
- [x] `src/crypto/crypto.go`: `WrapTenantKEK` / `UnwrapTenantKEK` (AES-256-GCM, tenant as AAD)
- [x] `src/crypto/crypto.go`: `RotateTenantKEK` — re-wrap CEKs under a new root key
- [x] `src/crypto/crypto.go`: `DeriveTenantOPRFShare(rootShare, tenantID)`

### Authorization
- [x] `src/storage/storage.go`: `acl` bucket; `BucketACL`/`ObjectACL`; `Authorize(tenantID, perm, resource) error` (default-deny, deny wins, admin bypass)
- [x] `src/transport/s3_communicator.go`: Extract tenant from Bearer/SigV4 auth token
- [x] `src/transport/s3_communicator.go`: Call `Authorize()` before routing every request

### Audit Logging
- [x] `src/common/struct.go`: `AuditLogEntry` struct
- [x] `src/storage/storage.go`: `audit_log` bucket; `WriteAuditLog()` appends with prev-hash chaining
- [x] `src/storage/storage.go`: `VerifyAuditLog() error` — walks the chain, validates hashes
- [x] `src/transport/s3_communicator.go`: `WriteAuditLog()` for GET/HEAD/PUT/DELETE/LIST
- [x] `src/transport/momo_tcp.go` / `momo_quic.go`: `WriteAuditLog()` in native LIST/GET/DELETE handlers
- [x] `src/server/file.go`: `WriteAuditLog()` for the shared PUT path (tenant-attributed)

### Config & Docs
- [x] `conf/momo.conf`: Document `[tenant.<id>]` + `[audit]` sections
- [x] `docs/GUIDES/CONFIGURATION.md`: Document tenant/ACL/audit config

## Testing

- [x] `TestTenantKeyDerivation` — unique keys per tenant, wrap/unwrap, rotation
- [x] `TestAuthorization` — allow/deny per ACL, cross-tenant isolation, default deny
- [x] `TestAuditLog_Chaining` — hash chain integrity
- [x] `TestAuditLog_TamperDetection` — tamper detection
- [x] `TestAuditLog_GetEntries` — retrieval by time range
- [x] `TestS3Communicator_WriteAuditLog` — audit integration records tenant-attributed ops
- [x] `make test` — full suite passes across all modules

## Compliance

- [x] OpenSpec change authored (Rule 73)
- [x] ADR generated via `make adr-sync` (Rules 77/78)
- [x] Blog post shipped — `053-multitenancy-authorization-audit.md` (Rule 76)
- [ ] PR body includes `Resolves #936` (Rule 11)
