# 0097-tenant-quota-enforcement

## Status
Accepted

## Confidence
High

## Context
`[tenant.<id>]` has accepted `quota_bytes` and `quota_objects` since R8, but
nothing reads them. A tenant can consume the whole node and the operator's quota
configuration is decorative. Documentation said so explicitly rather than
pretending, but a quota that does not limit anything is not a feature.

## Decision
- Per-tenant usage is accounted: The store SHALL maintain, per tenant, the number of bytes and logical objects charged to it, in a dedicated bucket, updated atomically with the limit check.
- A write that would exceed a quota is refused before it lands: The shared ingest path SHALL reserve the delta before reading the payload and SHALL refuse with a quota error when the reservation would exceed a configured limit.
- Overwrites pay only the delta: Storing a new value under an existing name SHALL adjust bytes by the difference and SHALL NOT increment the object count.
- Failed writes release their reservation: If the payload is rejected after the reservation (hash mismatch, checksum mismatch, storage failure), the reservation SHALL be reversed.
- Deletes release usage: A successful object delete SHALL charge the object's bytes and count back to the tenant.
- Quotas are opt-in and surface-scoped: Enforcement SHALL be a no-op when both quotas are unset, when the tenant is unknown or empty, and on surfaces that carry no tenant identity. The S3 gateway SHALL map a refusal to `403 QuotaExceeded`.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/070-tenant-quotas.md

## References
- Issue: #1315
- PR: 
- Spec: `openspec/changes/tenant-quota-enforcement/`
- Blog: docs/blog/posts/070-tenant-quotas.md

