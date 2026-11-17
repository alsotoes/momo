# No Blog Post: S3 Unsigned PUT Hash Derivation

This is a bug fix (issue #1346), not a new capability: the gateway already
accepted the upload, it just refused it later and failed silently. The
user-visible contract - "a PUT stores the object or returns an S3 error" - is
unchanged, so there is no architectural decision for a journal entry to explain.

Readers who want the mechanism have the requirements in
`openspec/changes/s3-unsigned-put-hash/`, ADR 0103, and the S3 section of
`docs/ARCHITECTURE.md`.
