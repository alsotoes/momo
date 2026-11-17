# No Blog Post: Multipart Part De-framing

This is a correctness and resource fix (issue #1358). The user-visible contract —
"a multipart upload stores the file you sent" — is unchanged; it just was not
true when the client framed its parts. There is no new capability or trade-off to
narrate.

Details: `openspec/changes/s3-multipart-deframing/`, ADR 0108, and the S3 section
of `docs/ARCHITECTURE.md`.
