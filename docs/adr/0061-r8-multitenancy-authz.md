# 0061-r8-multitenancy-authz

## Status
Proposed

## Confidence
Low

## Context
Current Momo uses a single shared `auth_token` for all clients and all operations. This is a single-tenant model with no isolation, no per-tenant quotas, no per-tenant keys, and no audit trail of who did what. Production deployments require:

1. **Tenant isolation** — each tenant's blobs, metadata, and keys are cryptographically separated
2. **Per-tenant keys** — master key per tenant, derived OPRF share, per-tenant auth tokens
3. **Authorization policies** — ACL/policy-based access control per bucket/object
4. **Audit logging** — immutable log of who (tenant+identity) did what (operation) when, with cryptographic integrity

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #936
- PR: 
- Spec: `openspec/changes/r8-multitenancy-authz/`
- Blog: 

