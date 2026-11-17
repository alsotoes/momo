# S3 Bucket-Relative Object Names

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1348

## Why

A path-style PUT stored the object under the **full path** (including the bucket
segment) whenever no `[storage] s3_bucket` was configured, while every read path
resolves the key by stripping the first segment. A real S3 client therefore
could not read back what it had just written: `put-object` returned 200 and
`head-object`/`get-object` answered 404, with `list-objects-v2` showing the
bucket-prefixed name.

Loading the metadata also used an empty object name as its "not parsed yet"
sentinel, so a request that legitimately parses to an empty key made the daemon
block waiting for another request on the same connection.

## What Changes

- Store the bucket-relative key whenever the path has one; a path with no bucket
  segment stays a legacy flat top-level object under its own name.
- Replace the empty-name sentinel with an explicit `metaParsed` flag.
- Align the `OPTIONS`-then-`PUT` fallback in `ReceiveMetadata` with the same
  naming rule.

## Impact

- Affected specs: `s3-bucket-keys`
- Affected code: `src/transport/s3_communicator.go`
- Behaviour: path-style PUTs resolve consistently; flat top-level objects are
  unchanged. Buckets remain implicit when `s3_bucket` is unset (no bucket
  registry exists), which is documented rather than silently implied.
