# Recover In-Flight Multipart Uploads After A Restart

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1371

## Why

Multipart sessions lived only in the process map. The parts were on disk
(#1358), but after a restart nothing referenced them:

- the client's `CompleteMultipartUpload` answered `404 NoSuchUpload`, so a
  multi-GiB upload had to start over;
- the spill directory looked like an orphan, so the reaper (#1361) eventually
  deleted data the client still believed it owned.

A restart during a large upload — deploy, OOM kill, node reboot — is routine, and
silently discarding the work is expensive and surprising.

## What Changes

- Each session writes a small manifest into its spill directory: bucket, key,
  creation time and the parts registered so far. It is written **after** the part
  file is in place and via temp-file-plus-rename, so it never advertises a part
  that does not exist and a crash mid-write cannot truncate it.
- At startup the gateway scans its spill root and rebuilds the session map, keeping
  upload ids stable so an in-flight `uploadId` keeps working.
- Recovery validates before trusting: an unparsable manifest, an empty bucket or
  key, or a missing/truncated part file means the session is **not** recovered
  (the reaper expires it instead). Expired sessions are never recovered — the TTL
  remains the single policy.
- The manifest is created at `CreateMultipartUpload`, so a restart between create
  and the first part also recovers.

## Impact

- Affected specs: `s3-multipart-recovery`
- Affected code: `src/transport/s3_multipart_manifest.go`,
  `src/transport/s3_communicator.go`, `src/transport/s3_multipart_reaper.go`
- Durability: a restart no longer loses an in-flight upload. A manifest write
  failure is logged and does not fail the request — only restart-recovery for that
  session is lost.
