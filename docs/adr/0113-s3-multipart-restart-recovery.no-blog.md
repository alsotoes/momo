# No Blog Post: Multipart Restart Recovery

A durability fix for an in-flight upload (issue #1371). The rule fits in one
sentence: the session is stored on disk next to its parts, so it comes back after a
restart. That completes the story the journal already tells about the spill
(#1358) and its expiry (#1361).

Details: `openspec/changes/s3-multipart-restart-recovery/`, ADR 0113, and the S3
section of `docs/ARCHITECTURE.md`.
