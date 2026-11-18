# S3 Object Tagging

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1375

## Why

Object tagging answered `501 NotImplemented` and `x-amz-tagging` on a PUT was
ignored. Tags are load-bearing in real pipelines: lifecycle rules filter on them,
cost allocation attributes by them, and `aws s3api put-object-tagging` is an
everyday command.

## What Changes

- `GET /{bucket}/{key}?tagging`, `PUT …?tagging`, `DELETE …?tagging`.
- `x-amz-tagging` on a plain `PUT` is accepted and stored, so `aws s3 cp --tagging`
  works.
- S3's rules are enforced: at most 10 tags, key 1–128 characters, value ≤256,
  unique keys, no control characters. A violation is `400 InvalidTag`, malformed
  XML `400 MalformedXML`.
- Tags are stored in the existing S3 metadata sidecar under the S3-native
  `x-amz-tagging` form (URL-encoded, key-sorted), so no new store surface is needed
  and replication carries tags with the rest of the object metadata.
- `?tagging` leaves the honest-501 object set. Bucket-root `?tagging` remains
  `501` — bucket tagging is a separate feature.

## Impact

- Affected specs: `s3-object-tagging`
- Affected code: `src/transport/s3_tagging.go`, `src/transport/s3_communicator.go`
- Compatibility: an object with no tags answers an empty `<TagSet>`, matching S3.
