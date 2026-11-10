# Tasks — Declarative Tenancy (#1325)

## Implementation

- [x] `src/common/struct.go`: `Configuration.ACLs map[string]*BucketACL`
- [x] `src/common/config.go`: `[acl.<bucket>]` parsing (`loadACLConfig`,
      `loadBucketACL`, `parseACLPermissions`), `!` prefix = deny, unknown
      permission/bucket/empty entries rejected at load
- [x] `src/storage/storage.go`: `ListBucketACLs`, `DeleteBucketACL`
- [x] `src/server/tenancy_bootstrap.go`: `bootstrapTenancy` — upsert tenants and
      bucket ACLs, prune only when the section declares entries, sorted for
      deterministic logging
- [x] `src/server/server.go`: bootstrap before `factory.Listen`, fatal on error

## Testing

- [x] `TestGetConfig_ACLParsing` — tenants, permissions, `!` deny, `*` wildcard
- [x] `TestGetConfig_ACLRejectsBadEntries` — unknown permission, empty list, no
      bucket, empty key, traversal in bucket name
- [x] `TestGetConfig_NoACLSectionsIsNotAnError` — section optional
- [x] `TestBootstrapTenancySeedsDeclaredEntries`
- [x] `TestBootstrapTenancyIsIdempotent`
- [x] `TestBootstrapTenancyPrunesRemovedEntries`
- [x] `TestBootstrapTenancyEmptySectionsNeverWipe`
- [x] `TestBootstrapTenancyPropagatesStoreErrors`
- [x] `TestBootstrapTenancyNilStoreIsANoOp`
- [x] `TestConfiguredTenantIsUsableEndToEnd` — real CAS store, real S3 handshake:
      declared tenant authenticates, declared ACL allows, undeclared bucket 403

## Docs & Compliance

- [x] `conf/momo.conf`: `[tenant.*]` quota note + `[acl.<bucket>]` sample
- [x] `docs/GUIDES/CONFIGURATION.md`: tenant seeding, ACL sections, prune rules
- [x] `docs/ARCHITECTURE.md` §4l: declarative tenancy
- [x] OpenSpec change authored (Rule 73)
- [x] ADR via `make adr-sync` (Rules 77/78)
- [x] Blog post (Rule 76)
