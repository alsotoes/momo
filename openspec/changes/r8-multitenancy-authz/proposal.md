# Change: R8 — Multi-Tenancy + Authorization + Audit

**Related Issues:**
- https://github.com/alsotoes/momo/issues/936 (R8: Multi-tenancy + authorization + audit)
- https://github.com/alsotoes/momo/issues/928 (Parent: Production readiness roadmap)

## Why

Current Momo uses a single shared `auth_token` for all clients and all operations. This is a single-tenant model with no isolation, no per-tenant quotas, no per-tenant keys, and no audit trail of who did what. Production deployments require:

1. **Tenant isolation** — each tenant's blobs, metadata, and keys are cryptographically separated
2. **Per-tenant keys** — master key per tenant, derived OPRF share, per-tenant auth tokens
3. **Authorization policies** — ACL/policy-based access control per bucket/object
4. **Audit logging** — immutable log of who (tenant+identity) did what (operation) when, with cryptographic integrity

## What Changes

### Tenant Model
- New `[tenants]` config section mapping `tenant_id` → `{ master_key_id, auth_token, quota_bytes, quota_objects, enabled }`
- Each tenant gets a unique `tenant_id` (UUID v4)
- Tenant metadata stored in dedicated BoltDB bucket `tenant_meta` with encrypted values

### Per-Tenant Keys
- Master key hierarchy: Root KEK (from config/env) → Tenant KEK (wrapped) → Tenant CEKs (per-object envelope encryption)
- OPRF share derivation: `DeriveTenantOPRFShare(tenant_id, root_oprf_share)` → per-tenant OPRF key
- Auth token: `tenant_id` + HMAC-SHA256(auth_secret) → opaque token
- Key rotation: `[tenants.<id>.key_rotation_interval]` triggers re-wrap of tenant KEK

### Authorization
- New ACL format in `bucket_meta` / `object_meta`: `acl: [{tenant_id, permissions: [read, write, delete, list, admin]}]`
- Default: bucket creator gets `admin`; explicit grants for cross-tenant access
- Policy engine: `authorize(tenant_id, operation, resource) -> allow/deny`
- S3 gateway: maps `Authorization: Bearer <tenant_token>` to tenant context

### Audit Logging
- Immutable append-only audit log in `audit_log` BoltDB bucket
- Entry: `{ts, tenant_id, identity, operation, resource, outcome, request_id}`
- `AUDIT:` prefix in structured logs for forensics
- Tamper-evidence: chained SHA-256 hashes of log entries (each entry includes prev hash)
- Retention: `[audit] retention_days` config (default 365)

## Non-Goals

- LDAP/OIDC/SAML integration (future identity provider tier)
- Fine-grained per-object ACLs beyond bucket-level (object-level future tier)
- Cross-region tenant replication (Phase 7 geo-distribution)
- Rate limiting / abuse protection (separate R7/R12 scope)

## Impact

- **Security:** Full tenant isolation at crypto + metadata + auth layers
- **Compliance:** Audit log enables SOC2/GDPR evidence
- **Operations:** Per-tenant quotas prevent noisy neighbors
- **S3 Compliance:** Per-tenant auth tokens work with existing S3 clients
- **Config:** New `[tenants]`, `[audit]` sections in `momo.conf`