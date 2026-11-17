# No Blog Post: Incomplete Multipart Upload Expiry

This is a resource-leak fix (issue #1361). The rule is simple enough to state in a
sentence: an abandoned upload stops holding disk after its TTL. There is no new
capability and no architectural trade-off to explain.

Details: `openspec/changes/s3-multipart-upload-expiry/`, ADR 0109, and the S3
section of `docs/ARCHITECTURE.md`.
