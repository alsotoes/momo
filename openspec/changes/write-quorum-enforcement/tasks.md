# Tasks — Write Quorum Enforcement (#1340)

## Implementation

- [x] `src/server/server.go`: count acknowledged replicas (local store + each
      forwarder that returned nil after the peer's ACK) with `forwardAcks`
- [x] `src/server/server.go`: withhold the success ACK when
      `1 + acks < write_quorum`, recording an AUDIT line and a replication failure
- [x] `src/common/config.go`: accept `write_quorum > 1` when `replication_order`
      contains a server-side forwarding mode (chain or splay)
- [x] `src/common/config.go`: refuse `write_quorum > 1` for client-side-only orders,
      because that path never collects replica acknowledgements

## Testing

- [x] `TestDaemonWriteQuorumDefaultStillAcks` — default quorum 1 still acks
- [x] `TestDaemonWriteQuorumUnmetWithholdsACK` — chain mode cannot reach quorum 3,
      so no ACK reaches the client
- [x] `TestGetConfig_R3DurabilityAndWriteQuorum` — quorum 2 accepted with
      `replication_order = 2,3,1`; refused for `3,0`; range still enforced
- [x] `TestGetConfig_UnimplementedKnobsAreRefused` — no longer lists `write_quorum`

## Docs & Compliance

- [x] `docs/GUIDES/CONFIGURATION.md`: what the quorum counts and its mode requirement
- [x] `docs/ARCHITECTURE.md`: quorum counts acknowledgements, not fsyncs
- [x] `docs/GUIDES/ROADMAP.md` R3 and `docs/CORE/COMPARISON.md` tunable consistency
- [x] `conf/momo.conf`: sample comments describe enforcement
- [x] OpenSpec change authored (Rule 73)
- [x] ADR generated via `make adr-sync` (Rules 77/78)
- [x] Blog post `076-write-quorum-that-counts.md` (Rule 76)
