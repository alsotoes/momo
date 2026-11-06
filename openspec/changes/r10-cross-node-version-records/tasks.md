# Tasks: R10 Version-Record Replication

## Implementation

- [x] `src/storage/versioning.go`: `PutVersionRecord` — caller-supplied record applied verbatim, idempotent by version ID, stale "latest" never demotes a newer record
- [x] `src/storage/versioning.go` + `storage.go`: `SetVersionReplicationHook` fired only for locally created versions/markers (under the store lock; async work required)
- [x] `src/p2p/types.go`: message types 22/23 (`MsgReplicateVersion`, `MsgReplicateVersionResponse`)
- [x] `src/p2p/metadata_rpc.go`: args/reply encoding, apply handler, reply correlation by request ID, timeout-bounded `ReplicateVersion` call, per-provider `RPCTimeout`
- [x] `src/server/version_replication.go`: placement-key selection (blob hash, object name for delete markers), replica fan-out excluding self, audit-logged failures
- [x] `src/server/server.go`: provider construction + hook installation during P2P bootstrap (metadata replication factor falls back to the global replication factor)

## Testing

- [x] Storage: verbatim apply, idempotent re-apply, stale-latest guard, delete-marker replication, hook fires once per local record and not for replicated ones, invalid input
- [x] P2P: payload round-trip, apply handler (versioning status + record + response), malformed payload ignored, acknowledged call, send error, timeout, peer rejection
- [x] Server: single-node no-op, fan-out to the non-self replica with the expected payload
- [x] `go test -race ./src/storage ./src/p2p ./src/server`, `make lint`, `make test`

## Compliance

- [x] OpenSpec change authored (Rule 73)
- [x] PR body references #1233 without closing the #938 umbrella
- [x] ADR generated via `make adr-sync` (Rule 77/78)
- [x] Blog post shipped (Rule 76) — post 061
