# 0021-bolt-http-lastmodified-appendformat

## Status
Accepted

## Confidence
High

## Context
`HandshakeServer` renders S3 HTTP `Last-Modified` response headers via
`formatHTTPLastModified`, which calls `time.Format()`. `.Format()` returns a
dynamically allocated string, so every S3 `GET` / `HEAD` / 304 / range response
pays one heap allocation on a hot request path — adding GC pressure under
sustained object-read workloads. The standard-library idiom `time.AppendFormat`
writes directly into the growing response byte slice, eliminating the
allocation entirely.

## Decision
- allocation-free Last-Modified header rendering: `S3Communicator.HandshakeServer` SHALL render the `Last-Modified` response header using `time.AppendFormat` directly into the growing response byte slice (`b = time.Unix(0, meta.ModTime).UTC().AppendFormat(b, http.TimeFormat)`), without calling `time.Format()` and without any dynamic string allocation for the timestamp.
- dead helper removal: The `formatHTTPLastModified` helper SHALL be removed once all call sites use `time.AppendFormat`.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/bolt-http-lastmodified-appendformat/
- Blog: docs/blog/posts/...md
