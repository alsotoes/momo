# Write Quorum Enforcement

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1340

Part of #1323.

## Why

`write_quorum` is documented as the number of replicas that must acknowledge a
write before the client is told it succeeded, but the daemon refused any value
above `1` (#1323) because replica acknowledgements were never collected. The
knob therefore could not strengthen durability at all.

The information was already there: a server-side replication forwarder returns
only after waiting for its peer's ACK (`client.ConnectStream` →
`sendFileStream` → `comm.ReceiveACK`), so a `nil` return means the peer stored
the blob. Counting those returns is enough to enforce a quorum without new wire
messages.

## What Changes

- The reception path counts acknowledged replicas: the local store is one, each
  successful forwarder is one more.
- When `1 + acks < write_quorum`, the success flag is not set, so no ACK is sent
  and the client observes a failed write instead of an unkept promise. The
  replication-failure counter and an AUDIT line record it.
- Config accepts `write_quorum > 1` when `replication_order` contains a
  server-side forwarding mode (chain or splay). Orders with only client-side
  modes (`primary-splay`, none) are refused at load, because the client-side
  fan-out does not collect replica acknowledgements — a quorum there could never
  be met.

## Impact

- Affected specs: write-quorum (new)
- Affected code: `src/server/server.go` (ack counting + enforcement),
  `src/common/config.go` (validation)
- Behaviour change only when `write_quorum > 1`; the default of `1` behaves
  exactly as before.
- Out of scope: read quorum; quorum for client-side `ReplicationPrimarySplay`
  (the native client logs per-replica failures instead of failing the upload);
  counting fsyncs (per-node durability stays `[storage] durability`).
