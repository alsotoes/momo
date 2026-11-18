# No Blog Post: S3 Object Tagging

Removes one entry from the honest-501 set (issue #1375). The design decision is
small and internal — tags ride the existing S3 metadata sidecar in S3's own wire
form — and the journal's S3-breadth narrative (posts 001/004/060) already explains
why the gateway implements the operations clients actually use.

Details: `openspec/changes/s3-object-tagging/`, ADR 0114, and the S3 section of
`docs/ARCHITECTURE.md`.
