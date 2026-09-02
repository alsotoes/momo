# 0014-add-quic-protocol

## Status
Accepted

## Confidence
High

## Context
Momo currently intertwines its communication protocol with its core replication logic. To support a pluggable architecture (Issue #131), we must separate **how** nodes communicate from **what** they do with the data. This spec focuses on implementing the **Momo-QUIC** variant and the underlying **Protocol Factory**, while the **S3** variant is tracked separately in `openspec/changes/add-s3-protocol/`.

## Decision
- Decoupled Communication Architecture (Issue #131): The system SHALL strictly separate the communication protocol from the core replication logic.
- Unified 'Communicator' Abstraction: The system SHALL implement a `Communicator` interface that encapsulates transport-specific connection management and protocol-specific handshaking.
- Robust Stack Configuration: The system SHALL utilize a single composite `protocol` string in the `[global]` section of `momo.conf` to configure the network stack.
- Universal QUIC/TCP Coexistence (Issue #132): The daemons SHALL support simultaneous listening for both TCP and QUIC packets.

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
- Spec: openspec/changes/add-quic-protocol/
- Blog: docs/blog/posts/...md
