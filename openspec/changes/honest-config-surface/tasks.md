# Tasks — Honest Configuration Surface (#1323)

## Implementation

- [x] `src/common/config.go`: refuse an explicitly configured `write_quorum > 1`,
      `metadata_ttl` and `lease_timeout` with `ENOTSUP` + issue link; defaults
      unaffected (`HasKey` gate, so `section.Key()` materialisation does not
      trigger it)
- [x] `src/p2p/metadata_rpc.go`: `MetadataRPCProvider.SetWriteQuorum` +
      `writeQuorum(replicas)` used by both the write and the delete path, clamped
      to the replica count, zero = strict majority
- [x] `src/server/version_replication.go`: wires `cfg.Momofs.MetadataQuorum`
- [x] `conf/momo.conf`: the three keys are marked NOT IMPLEMENTED, `write_quorum`
      carries the explanation inline

## Testing

- [x] `TestGetConfig_UnimplementedKnobsAreRefused` — each refused key names itself
      and the issue
- [x] `TestGetConfig_UnimplementedKnobDefaultsStayUsable` — defaults still load
- [x] `TestGetConfig_MetadataQuorumIsAccepted` — default majority and explicit value
- [x] `TestGetConfig_R3DurabilityAndWriteQuorum` — updated: `2` refused, `1` accepted
- [x] `TestWriteQuorumOverride` — override, clamp, reset, negative, single replica
- [x] `TestMajorityQuorumUnchanged` — shared helper unchanged for 0..5

## Docs & Compliance

- [x] `docs/GUIDES/CONFIGURATION.md`: implemented-vs-not-implemented notes
- [x] `docs/ARCHITECTURE.md`: metadata quorum is configurable
- [x] OpenSpec change authored (Rule 73)
- [x] ADR via `make adr-sync` (Rules 77/78)
- [x] Blog post (Rule 76)
