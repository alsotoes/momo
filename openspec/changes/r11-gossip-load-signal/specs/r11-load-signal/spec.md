# Spec: gossip load signal

## Purpose

Give every node a locally available, bounded view of peer load so a future
admission/redirect rule can compare nodes without a coordinator or per-request
fan-out.

## Requirements

### Requirement: Fixed-size load payload
The system SHALL encode a load report in a fixed 20-byte payload containing disk
utilisation percent, in-flight requests, replication queue depth, and a sample
timestamp.

#### Scenario: Round trip
- GIVEN a report with 87% disk, 42 in-flight, 7 queued
- WHEN it is encoded and decoded
- THEN the decoded values equal the input
- AND the encoded length is 20 bytes

#### Scenario: Malformed input is rejected
- GIVEN a payload shorter than 20 bytes, longer than the maximum payload, or
  claiming more than 100% disk
- WHEN it is decoded
- THEN decoding fails with a POSIX-mapped error
- AND no value is stored

### Requirement: Publication rides the heartbeat fanout
The system SHALL publish the local report to the same peers that receive a
heartbeat, on the same cadence, and only when a load provider is installed.

#### Scenario: Provider installed
- GIVEN a gossiper with a load provider and one known peer
- WHEN a heartbeat is sent
- THEN exactly one load message is sent to that peer with the local node as sender

#### Scenario: No provider
- GIVEN a gossiper without a load provider
- WHEN a heartbeat is sent
- THEN no load message is sent

### Requirement: Peers store the latest report
The system SHALL store the most recent valid report on the peer record and
expose it by peer ID.

#### Scenario: Latest report wins
- GIVEN a peer with no earlier report
- WHEN a valid report arrives
- THEN it is returned for that peer
- AND unknown peers report no value

#### Scenario: Invalid report ignored
- GIVEN a peer with a stored report
- WHEN a malformed report arrives
- THEN the stored value is unchanged

### Requirement: Peer load is exported as metrics
The system SHALL export the last report per peer as `momo_peer_disk_used_percent{peer}`
and `momo_peer_inflight_requests{peer}` gauges, and SHALL declare the families
even before any report arrives.

#### Scenario: Reports present
- GIVEN peers 2 and 3 reported 91%/17 and 12%/0
- WHEN the metrics endpoint is scraped
- THEN both peers appear with their reported values

#### Scenario: No reports yet
- GIVEN no peer has reported
- WHEN the metrics endpoint is scraped
- THEN no per-peer samples are emitted
- AND both family declarations are still present

## Out of scope

- Any redirect or admission decision (tracked in #1261).
- Replication queue depth accounting (field reserved, reports zero today).
