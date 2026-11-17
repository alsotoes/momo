# No Blog Post: S3 Checksum-Mode Response Headers

This is a conformance fix (issue #1352): the gateway returned a header it should
only return on request, and a standard client rejected the download. There is no
new capability and no architectural choice to explain.

Details live in `openspec/changes/s3-checksum-mode-response/`, ADR 0105 and the S3
section of `docs/ARCHITECTURE.md`.
