# Change: External Client Replication Mode Downgrade
**Related Issues:**
- https://github.com/alsotoes/momo/issues/258

## Why
When external S3 clients (e.g., aws-cli) connect to a Momo server, they do not
send `X-Momo-Requested-Mode` or `X-Momo-Timestamp` headers. The server mistakenly
treats them as forwarded peer connections (because `X-Amz-Date` parses to a valid
timestamp ≠ `DummyEpoch`) and uses `ReplicationNone` — no replication occurs.

Additionally, even if the server used its configured mode, `primary-splay` (mode 3)
requires the *client* to fan out to replicas. External S3 clients cannot do this.

## What Changes
- Add `client_side_replication_modes` config variable (comma-separated list of
  mode IDs that require a momo-aware client).
- Detect external S3 clients in `s3_communicator.go` (absence of
  `X-Momo-Requested-Mode`) and force `timestamp = DummyEpoch` so the server uses
  its configured replication mode.
- In `server.go`, after mode selection, if the connection is external and the
  selected mode is in `client_side_replication_modes`, walk `replication_order`
  forward to find the next server-side mode. Use that for this transaction only.
- Parse `client_side_replication_modes` in `config.go` with the same zero-alloc
  CSV parser used for `replication_order`.

## Impact
- Affected specs: `networking`, `storage`
- Affected code: `src/transport/s3_communicator.go`, `src/server/server.go`,
  `src/common/config.go`, `src/common/struct.go`, `conf/momo.conf`
