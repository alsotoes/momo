> GitHub Issue URL: https://github.com/alsotoes/momo/issues/936

# Spec: R8 — Multi-Tenancy + Authorization + Audit

## Requirements

### R8-T1: Tenant Isolation

**Requirement:** Each tenant's data MUST be cryptographically isolated from other tenants.

**Scenario: Tenant A cannot read Tenant B's blobs**

Given two tenants A and B with distinct master keys,
When tenant A attempts to `GET` a blob owned by tenant B,
Then the request is denied with `403 Forbidden`.

**Scenario: Tenant metadata encryption**

Given a tenant's metadata (bucket names, object names, sizes),
When stored in BoltDB,
Then it is encrypted with the tenant's CEK and unreadable without the tenant's KEK.

### R8-T2: Per-Tenant Key Hierarchy

**Requirement:** Each tenant MUST have an independent key hierarchy derivable from the root KEK.

**Scenario: Tenant key derivation**

Given a root KEK and tenant ID,
When `DeriveTenantKeys(tenant_id)` is called,
Then it returns a unique tenant KEK, OPRF share, and auth token,
And no two tenants share any key material.

**Scenario: Key rotation**

Given `[tenants.<id>.key_rotation_interval] = "90d"`,
When the interval elapses,
Then the tenant's KEK is re-wrapped with a new version,
And existing CEKs are re-wrapped (lazy or eager).

### R8-T3: Authorization Policy

**Requirement:** Every operation MUST be authorized against the tenant's ACL/policy.

**Scenario: Cross-tenant bucket access**

Given tenant A owns bucket `b`, and tenant B has `read` permission on `b`,
When tenant B `GET`s an object in `b`,
Then the request is allowed.

**Scenario: Unauthorized access denied**

Given tenant C has no permissions on bucket `b`,
When tenant C `PUT`s to `b`,
Then the request is denied with `403 Forbidden`.

### R8-T4: Audit Logging

**Requirement:** Every authenticated operation MUST produce an immutable audit entry.

**Scenario: Write operation logged**

Given a tenant performs `PUT /bucket/key`,
When the operation completes,
Then the audit log contains an entry with:
- `ts` (UnixNano), `tenant_id`, `identity`, `operation: "PUT"`,
- `resource: "bucket/key"`, `outcome: "success"`, `request_id`

**Scenario: Tamper detection**

Given an attacker modifies the audit log on disk,
When `VerifyAuditLog()` runs,
Then it detects the hash chain break and reports corruption.

## Non-Goals

- LDAP/OIDC integration
- Per-object ACLs (bucket-level only for R8)
- Cross-region tenant replication
- Rate limiting (R7 scope)