# Honest Configuration Surface

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1323

## Why

Auditing every `Configuration*` field for read sites outside the loader found four
keys the daemon accepts and never honours. A key that is parsed and validated but
never read is worse than a missing key: it advertises a guarantee the system does
not have. The worst of the four is `write_quorum`, whose documentation promises a
write fails when fewer than N durable replicas acknowledge — while replica
acknowledgements are not collected at all.

## What Changes

- **`metadata_quorum` is wired**: `MetadataRPCProvider` takes the configured
  acknowledgement count for metadata writes *and* deletes, clamped to the replica
  count, with zero keeping the strict-majority default.
- **`write_quorum > 1`, `metadata_ttl` and `lease_timeout` are refused at load**
  with an explicit "not implemented yet" error naming the tracking issue. Only an
  explicitly configured key is refused; the defaults keep working.
- The dead `MetadataRPCTTL` constant (a TTL for a cache that does not exist) is
  removed, and the sample config stops presenting the three keys as usable.

## Impact

- Affected specs: config-honesty (new)
- Affected code: `src/common/config.go` (validation),
  `src/p2p/metadata_rpc.go` (quorum override),
  `src/server/version_replication.go` (wiring), `conf/momo.conf`
- Backward compatible for every deployment that does not set the three refused
  keys; a deployment that *does* set them now fails at startup instead of
  silently getting no behaviour.
