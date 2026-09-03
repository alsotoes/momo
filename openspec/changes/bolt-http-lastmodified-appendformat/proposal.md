# Change: Bolt — eliminate time formatting allocations in S3 HTTP headers

**Related Issues:**
- https://github.com/alsotoes/momo/issues/977 (tracking)

## Why

`HandshakeServer` renders S3 HTTP `Last-Modified` response headers via
`formatHTTPLastModified`, which calls `time.Format()`. `.Format()` returns a
dynamically allocated string, so every S3 `GET` / `HEAD` / 304 / range response
pays one heap allocation on a hot request path — adding GC pressure under
sustained object-read workloads. The standard-library idiom `time.AppendFormat`
writes directly into the growing response byte slice, eliminating the
allocation entirely.

## What Changes

- In `S3Communicator.HandshakeServer` (`src/transport/s3_communicator.go`):
  replace the three `lastModifiedStr` / `formatHTTPLastModified` usages in the
  GET (200 + 304) and HEAD header builds with
  `b = time.Unix(0, meta.ModTime).UTC().AppendFormat(b, http.TimeFormat)`.
- Remove the now-unused `formatHTTPLastModified` helper.
- Keep the emitted header bytes byte-for-byte identical (same
  `http.TimeFormat` layout), so AWS SDKs and aws-cli parsing is unchanged.
- Append a `.jules/bolt.md` learning entry documenting the pattern
  (Rule 44 append-only).

## Non-Goals

- The XML `LastModified` rendering paths already use the
  `AppendFormat` + stack-buffer idiom (shipped via
  `bolt-s3-copyresult-time-alloc`); they are out of scope here.
- No wire/protocol changes, no config changes.

## Impact

- **Affected Specs:** `specs/bolt-http-lastmodified-appendformat/spec.md`
  (requirements below).
- **Performance:** One fewer heap string allocation per GET/HEAD/304 response.
- **Correctness:** Header output identical; all existing S3 tests must pass.