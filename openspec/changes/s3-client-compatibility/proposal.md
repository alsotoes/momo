# S3 Client Compatibility Check

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1350

## Why

The gateway's S3 surface is verified with hand-written curl requests and unit
tests, which encode our own understanding of the protocol. Two real defects
reached `master` because of that gap: an unsigned PUT that reset the connection
(#1346) and a path-style PUT whose object could never be read back (#1348). Both
are trivial for a standard S3 client to expose.

## What Changes

- Add `.github/scripts/test-s3-client-compat.sh`: boots a 3-node `s3-tcp`
  cluster and drives it with **aws-cli** — put/head/get with a byte comparison,
  key-only listing, copy, a 20 MiB multipart `s3 cp`, and delete.
- Add `.github/workflows/s3_client_compat.yml` (`make smoke-s3-client`) running
  it on master and on PRs touching `src/transport` or `src/server`.
- The script skips cleanly when aws-cli is not installed.

## Impact

- Affected specs: `s3-client-compat`
- Affected code: `.github/scripts/`, `.github/workflows/`, `Makefile`, docs
- No product code changes.
