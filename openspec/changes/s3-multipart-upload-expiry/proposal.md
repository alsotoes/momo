# Incomplete Multipart Uploads Expire

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1361

## Why

An incomplete multipart upload held two kinds of state with no expiry: an
in-memory session in the package-level `uploads` map, and a per-upload spill
directory holding every uploaded part (#1358). A client that uploads parts and
never completes or aborts — a crashed tool, an interrupted `aws s3 cp`, a retried
pipeline — leaked both for the life of the daemon. A daemon killed mid-upload
leaked the directory with no session at all.

Real S3 bounds this with an `AbortIncompleteMultipartUpload` lifecycle rule; the
gateway has no lifecycle support for it and no other bound.

## What Changes

- Reap sessions older than `[storage] multipart_upload_ttl` (Go duration,
  default `24h`, `0` disables): remove the spill directory, drop the session,
  write an AUDIT line.
- Sweep orphaned `momo-s3-multipart-*` directories in the temp root that no live
  session owns and that are older than the TTL — the daemon-killed case.
- Run the sweep at startup and every ten minutes, context-cancelled and
  panic-guarded, wired through `startBackgroundMaintenance`.
- Negative or unparseable TTL values are refused at config load.

## Impact

- Affected specs: `s3-multipart-expiry`
- Affected code: `src/transport/s3_multipart_reaper.go`,
  `src/server/background_maintenance.go`, `src/common/{config,struct}.go`
- Disk: an abandoned upload now stops consuming space after the TTL instead of
  forever. A live upload is never touched: the orphan sweep skips directories a
  session still owns.
