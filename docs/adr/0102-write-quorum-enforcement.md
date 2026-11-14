# 0102-write-quorum-enforcement

## Status
Accepted

## Confidence
High

## Context
`write_quorum` is documented as the number of replicas that must acknowledge a
write before the client is told it succeeded, but the daemon refused any value
above `1` (#1323) because replica acknowledgements were never collected. The
knob therefore could not strengthen durability at all.

The information was already there: a server-side replication forwarder returns
only after waiting for its peer's ACK (`client.ConnectStream` →
`sendFileStream` → `comm.ReceiveACK`), so a `nil` return means the peer stored
the blob. Counting those returns is enough to enforce a quorum without new wire
messages.

## Decision
- Acknowledged Replica Counting: The daemon SHALL count the local store as one acknowledged replica and SHALL count each replication forwarder that completed without error as one additional acknowledged replica, because such a forwarder has already received the peer's ACK.
- Quorum Enforcement: When `write_quorum` is greater than 1 the daemon SHALL withhold the success ACK if the acknowledged replica count is below the configured quorum, so the client observes a failed write.
- Configuration Validity: The daemon SHALL accept `write_quorum` in `[1, replication_factor]` and SHALL refuse at load a value above 1 unless `replication_order` contains a server-side forwarding mode (chain or splay).

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/076-write-quorum-that-counts.md

## References
- Issue: #1340
- PR: 
- Spec: `openspec/changes/write-quorum-enforcement/`
- Blog: docs/blog/posts/076-write-quorum-that-counts.md

