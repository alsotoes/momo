# 0093-declarative-tenancy

## Status
Accepted

## Confidence
High

## Context
R8's tenancy layers read state that nothing wrote outside tests. `[tenant.<id>]`
sections were parsed into `Configuration.Tenants` but never persisted, while the
S3 handshake resolves a tenant from the `tenant_meta` bucket — so a tenant declared
in `momo.conf` authenticated as unknown and got `403`. ACLs had no surface at all:
the types, the store methods and the default-deny `Authorize` existed, but no
config key, CLI command or admin route could create an entry. Multi-tenancy was
therefore unreachable in production, which is the same failure mode as #1316
(a field many layers read, never assigned) and #1323 (a key accepted, never read).

## Decision
- Tenants declared in configuration authenticate: Every `[tenant.<id>]` entry SHALL be persisted to the tenant store at startup, so its `auth_token` resolves during the S3 handshake.
- Bucket ACLs are declarable: `[acl.<bucket>]` sections SHALL populate bucket ACLs: each key is a tenant ID and each value a comma-separated permission list, where a permission prefixed with `!` is stored as an explicit deny.
- Bootstrap is idempotent and conservative: Seeding SHALL be safe to repeat and SHALL NOT remove state it did not declare.
- Invalid declarations fail at load: An ACL declaration SHALL be rejected when it names an unknown permission, an invalid bucket, an empty tenant key or an empty permission list.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/073-tenancy-you-can-write-down.md

## References
- Issue: #1325
- PR: 
- Spec: `openspec/changes/declarative-tenancy/`
- Blog: docs/blog/posts/073-tenancy-you-can-write-down.md

