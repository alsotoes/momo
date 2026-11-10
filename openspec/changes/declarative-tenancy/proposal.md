# Declarative Tenancy

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1325

## Why

R8's tenancy layers read state that nothing wrote outside tests. `[tenant.<id>]`
sections were parsed into `Configuration.Tenants` but never persisted, while the
S3 handshake resolves a tenant from the `tenant_meta` bucket — so a tenant declared
in `momo.conf` authenticated as unknown and got `403`. ACLs had no surface at all:
the types, the store methods and the default-deny `Authorize` existed, but no
config key, CLI command or admin route could create an entry. Multi-tenancy was
therefore unreachable in production, which is the same failure mode as #1316
(a field many layers read, never assigned) and #1323 (a key accepted, never read).

## What Changes

- New `[acl.<bucket>]` section: each key is a tenant ID, each value a
  comma-separated permission list (`read`, `write`, `delete`, `admin`), with a
  leading `!` recording an explicit **deny**, which finally makes the tri-state
  evaluation from #1313 reachable from configuration.
- Startup bootstrap, before the listener opens, idempotent: declared tenants are
  upserted into `tenant_meta`, declared bucket ACLs into the ACL bucket, and
  entries no longer declared are pruned **only when that configuration section
  declares at least one entry** (an absent section never wipes state).
- A bootstrap failure is fatal: running with unusable tenancy would deny every
  tenant while looking healthy.

## Impact

- Affected specs: tenancy-config (new)
- Affected code: `src/common/config.go` (ACL sections), `src/common/struct.go`
  (`Configuration.ACLs`), `src/storage/storage.go` (`ListBucketACLs`,
  `DeleteBucketACL`), `src/server/tenancy_bootstrap.go` (new),
  `src/server/server.go` (wiring), `conf/momo.conf`, docs
- Backward compatible: without `[tenant.*]`/`[acl.*]` sections nothing is seeded
  and nothing is pruned.
