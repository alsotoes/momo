# No Blog Post: UploadPartCopy

This removes one entry from the honest-501 set (issue #1365): the gateway now does
the S3 operation instead of refusing it. Server-side copy has no new design
decision — it reuses `CopyObject`'s source resolution and the multipart part spill
from #1358 — so a post would restate the S3-breadth narrative already told in
posts 001/004/060.

Details: `openspec/changes/s3-upload-part-copy/`, ADR 0111, and the S3 section of
`docs/ARCHITECTURE.md`.
