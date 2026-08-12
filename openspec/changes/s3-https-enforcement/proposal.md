# Change: S3 — enforce HTTPS (or gated insecure) for the S3 storage backend endpoint
**Related Issues:**
- https://github.com/alsotoes/momo/issues/774

## Why
The S3 backend client (`S3BlobStore.S3Endpoint`) accepts any scheme (`http://` or `https://`) without validation, sending SigV4 credentials, blob content, and object metadata in cleartext when the endpoint is `http://`. AWS requires TLS for S3; the current highest practice is HTTPS-only plus bucket encryption at rest.

## What Changes
- **`NewS3BlobStore` validates the endpoint scheme.** Before any request is issued:
  - `https://` — always accepted.
  - `http://` — **rejected with an `EINVAL` config error** unless the new `s3_insecure = true` config flag is set, in which case a prominent `WARNING` is logged at startup.
  - Missing scheme or unsupported schemes (e.g. `ftp://`, no scheme) — rejected with `EINVAL`.
- **New config knob `s3_insecure`.** Added to `ConfigurationStorage` struct and parsed from the `s3_insecure` INI key. Defaults to `false`.
- **Documentation updates:** Config guide (`docs/CONFIGURATION.md`) documents `s3_insecure`; protocol doc (`docs/PROTOCOL.md`) documents the layered confidentiality model (at-rest AES-GCM-256 + outbound TLS + inbound gateway TLS).

## Non-Goals
- No change to inbound gateway TLS enforcement (separate issue #775).
- No change to the momo wire protocol or storage layer internals — only the outbound endpoint scheme is validated.

