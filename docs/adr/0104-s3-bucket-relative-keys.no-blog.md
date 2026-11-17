# No Blog Post: S3 Bucket-Relative Object Names

This is a correctness fix (issue #1348) for a bug that made a standard S3 client
unable to read back what it had just written. There is no new capability and no
architectural choice for a journal entry to explain: the naming rule now matches
what the read paths already assumed.

The mechanism lives in `openspec/changes/s3-bucket-relative-keys/`, ADR 0104, and
the S3 section of `docs/ARCHITECTURE.md`.
