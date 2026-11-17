# No Blog Post: S3 Client Compatibility Check

This adds a CI check, not a capability users can adopt. There is no product
behaviour to explain: the script boots a cluster and drives it with `aws-cli`, and
`docs/GUIDES/TESTING.md` documents how to run it.

Context for the decision lives in issue #1350, ADR 0105 and
`openspec/changes/s3-client-compatibility/`.
