# No Blog Post: Composite Checksum Omission

A conformance fix (issue #1354): the gateway returned a multipart checksum that
cannot describe the object, so a standard client aborted the download. No new
capability, no architectural decision to narrate.

Details: `openspec/changes/s3-composite-checksum-omission/`, ADR 0107, and the S3
section of `docs/ARCHITECTURE.md`.
