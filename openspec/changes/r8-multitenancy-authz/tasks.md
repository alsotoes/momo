# Tasks: R8 — Multi-Tenancy + Authorization + Audit

## Implementation

### Tenant Model + Config
- [ ] `src/common/struct.go`: Add `TenantConfig` struct with `ID`, `MasterKeyID`, `AuthToken`, `QuotaBytes`, `QuotaObjects`, `Enabled`
- [ ] `src/common/config.go`: Parse `[tenants]` section; validate required fields
- [ ] `src/storage/storage.go`: Add `tenant_meta` bucket; tenant CRUD methods

### Per-Tenant Key Hierarchy
- [ ] `src/crypto/crypto.go`: Add `DeriveTenantKeys(rootKEK, tenantID) (KEK, OPRFShare, AuthToken)`
- [ ] `src/crypto/crypto.go`: Add `WrapTenantKEK(rootKEK, tenantKEK)` / `UnwrapTenantKEK`
- [ ] `src/crypto/crypto.go`: Add `RotateTenantKEK(tenantID, newRootKEK)` — re-wrap CEKs
- [ ] `src/crypto/crypto.go`: Add `DeriveTenantOPRFShare(rootShare, tenantID)`

### Authorization
- [ ] `src/storage/storage.go`: Add `acl` field to `bucket_meta` (JSON: `[]ACLEntry{TenantID, Permissions}`)
- [ ] `src/storage/storage.go`: Add `Authorize(tenantID, operation, resource) error`
- [ ] `src/transport/s3_communicator.go`: Extract tenant from `Bearer <token>`; inject into request context
- [ ] `src/transport/s3_communicator.go`: Call `Authorize()` in each handler

### Audit Logging
- [ ] `src/common/log.go`: Add `AuditLogEntry` struct + `WriteAuditLog(entry)` with hash chaining
- [ ] `src/storage/storage.go`: Add `audit_log` bucket; `WriteAuditLog()` appends with prev-hash
- [ ] `src/transport/s3_communicator.go`: Call `WriteAuditLog()` in every handler
- [ ] `src/transport/momo_tcp.go` / `momo_quic.go`: Call `WriteAuditLog()` in native handlers
- [ ] `src/common/log.go`: Add `VerifyAuditLog() error` — walks chain, validates hashes

### Config
- [ ] `src/common/config.go`: Parse `[tenants]`, `[audit]` sections
- [ ] `conf/momo.conf`: Document `[tenants]` + `[audit]` sections

## Testing

- [ ] `TestTenantKeyDerivation` — verifies unique keys per tenant
- [ ] `TestAuthorization` — verifies allow/deny per ACL
- [ ] `TestAuditLogChaining` — verifies hash chain integrity
- [ ] `TestAuditLogTamperDetection` — modifies log, verifies `VerifyAuditLog()` fails
- [ ] `go test -race ./...` — all tests pass
- [ ] `make test` — full suite passes

## Compliance

- [ ] OpenSpec change authored (Rule 73)
- [ ] ADR generated via `make adr-sync` (Rule 77/78)
- [ ] Blog post shipped (Rule 76)
- [ ] PR body includes `Resolves #936` (Rule 11)