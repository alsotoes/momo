# Tenant Quota Enforcement

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1315

## Why

`[tenant.<id>]` has accepted `quota_bytes` and `quota_objects` since R8, but
nothing reads them. A tenant can consume the whole node and the operator's quota
configuration is decorative. Documentation said so explicitly rather than
pretending, but a quota that does not limit anything is not a feature.

## What Changes

- **Per-tenant accounting** in a `tenant_usage` bucket: bytes and object count,
  updated in the same bbolt transaction that checks the limit, so a burst of
  concurrent writes at the boundary cannot overshoot.
- **Pre-write enforcement** on the shared ingest path: the reservation is applied
  before the payload is read, so an over-quota write is refused without consuming
  disk, and released if the write later fails or is rejected.
- **Overwrite delta accounting**: replacing an object charges the size difference
  and never adds to the object count; shrinking an object frees bytes.
- **Delete release**: a successful S3 delete charges the object's bytes and count
  back to the tenant.
- **Honest scope**: accounting counts logical objects per tenant (two names that
  deduplicate to one blob still count as two), and it is an S3-gateway boundary —
  the native protocols carry no tenant identity, so they are never charged.

## Impact

- Affected specs: tenant-quotas (new)
- Affected code: `src/storage/quota.go` (new), `src/storage/storage.go` (bucket),
  `src/server/file.go` (reservation), `src/transport/s3_communicator.go`
  (403 mapping, delete release)
- Backward compatible: both quotas unset, an unknown tenant, or a tenant-less
  surface make every call a no-op.
