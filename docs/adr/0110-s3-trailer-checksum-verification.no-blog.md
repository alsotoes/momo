# No Blog Post: Trailing Checksum Verification

This closes a documented ceiling (issue #1363): the gateway already accepted the
aws-chunked trailer form, it just threw the value away. Verifying it is the
completion of integrity checksums (#820), already explained in post 012, so a new
post would repeat that narrative.

Details: `openspec/changes/s3-trailer-checksum-verification/`, ADR 0110, and the S3
section of `docs/ARCHITECTURE.md`.
