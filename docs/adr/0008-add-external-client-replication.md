# 0008-add-external-client-replication

## Status
Accepted

## Confidence
High

## Context
When external S3 clients (e.g., aws-cli) connect to a Momo server, they do not
send `X-Momo-Requested-Mode` or `X-Momo-Timestamp` headers. The server mistakenly
treats them as forwarded peer connections (because `X-Amz-Date` parses to a valid
timestamp ≠ `DummyEpoch`) and uses `ReplicationNone` — no replication occurs.

Additionally, even if the server used its configured mode, `primary-splay` (mode 3)
requires the *client* to fan out to replicas. External S3 clients cannot do this.

## Decision
- External S3 Client Detection: The system SHALL detect external S3 clients by the absence of the `X-Momo-Requested-Mode` header and treat them as direct client connections (equivalent to `DummyEpoch` timestamp).
- Configurable Client-Side Replication Modes: The system SHALL support a `client_side_replication_modes` config variable containing a comma-separated list of replication mode IDs that require a momo-aware client. Any mode in this list is automatically skipped for external S3 clients via set subtraction: `effective_modes = replication_order \ client_side_replication_modes`
- Per-Transaction Downgrade Without Global Mutation: The per-connection mode downgrade MUST NOT mutate the global polymorphic replication state. The downgrade applies to the current transaction only.

## Consequences


## Alternatives Considered
None documented.

## Confidence
High

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/add-external-client-replication/
- Blog: docs/blog/posts/...md
