# R11 step 1: gossip load signal

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1261

## Why

R11 can move cold bytes off a full node, but it cannot avoid *sending work* to a
full node: no node knows how loaded its peers are. Membership gossip carries
alive/suspect/offline and round-trip time, not utilisation, so any redirect rule
would have to guess or fan out a query per request.

## What Changes

Every node publishes a small, fixed-size load summary to the peers it already
gossips with, on the existing heartbeat cadence:

- disk utilisation percent of the data filesystem,
- in-flight requests,
- replication queue depth (reserved; currently zero),
- the sample timestamp.

Peers store the latest report on the peer record. Nothing acts on it yet: the
redirect contract (S3 307 vs native advisory) remains open in #1261 and is
deliberately out of scope here.

## Constraints

- Fixed 20-byte payload; rejected if oversized or if the disk value exceeds 100.
- No new ticker and no new connection: the report rides the heartbeat fanout.
- Publication is optional (nil provider = no messages), so tests and
  single-node deployments are unaffected.
