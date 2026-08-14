# Peer-Quality-Aware Quorum Selection

## Purpose

This specification makes quorum selection in the P2P layer quality-aware: the
scatter-gather, lease, and threshold-OPRF quorums prefer live, low-RTT peers
and exclude suspect/offline peers, by ranking peers on per-peer EWMA RTT.

## ADDED Requirements

### Requirement: Per-Peer RTT Tracking

Each `Peer` SHALL expose an EWMA round-trip time via `SetRTT(dur)` and
`RTT()` accessors. The gossiper SHALL write its ping-derived EWMA sample to the
target peer so `Peer.RTT()` reflects fresh liveness data.

#### Scenario: RTT is recorded and readable per peer
- **GIVEN** a peer `P` and an EWMA sample `d`
- **WHEN** `P.SetRTT(d)` is called
- **THEN** `P.RTT()` returns `d` and the value is durable across concurrent reads.

### Requirement: Quality-Aware Alive Selection

`PeerMap` SHALL provide `AliveByQuality()` returning the alive peers (excluding
`Suspect` and `Offline`) sorted by RTT ascending (best first). Peers with
unknown RTT (0) SHALL sort after known-RTT peers but remain included while
alive.

#### Scenario: low-RTT peers rank before high-RTT peers
- **GIVEN** alive peers `A` (RTT 5ms), `B` (RTT 50ms), `C` (unknown RTT)
- **WHEN** `AliveByQuality()` is called
- **THEN** the ordering is `A, B, C` (ascending RTT; unknown last).

#### Scenario: suspect and offline peers are excluded
- **GIVEN** one alive peer `A`, one `Suspect` peer `B`, one `Offline` peer `C`
- **WHEN** `AliveByQuality()` is called
- **THEN** it returns only `A`.

### Requirement: Quorum Consumers Use Quality Ordering

The scatter-gather query, lease acquire, and OPRF evaluation paths SHALL select
their quorum from `AliveByQuality()` so the lowest-RTT alive peers are
preferred.

#### Scenario: scatter-gather prefers the low-RTT peer
- **GIVEN** two alive peers with distinct RTTs
- **WHEN** a scatter-gather query runs
- **THEN** the low-RTT peer is contacted first.

#### Scenario: lease quorum prefers low-RTT peers
- **GIVEN** a lease quorum of size `N` and several alive peers with distinct RTTs
- **WHEN** a lease is acquired
- **THEN** the `N` lowest-RTT peers form the preferred quorum.

#### Scenario: OPRF evaluation prefers low-RTT peers
- **GIVEN** a threshold-OPRF evaluation among multiple alive peers
- **WHEN** evaluations are collected
- **THEN** the lowest-RTT peers are preferred for key-share evaluation.

### Requirement: Wire Stability

This change SHALL NOT add, remove, or reinterpret any wire message type, payload
field, or byte layout (Rules 7, 38). Peer quality is a local in-memory ranking.

#### Scenario: no protocol bytes change
- **GIVEN** no configuration change
- **WHEN** P2P RPCs are exchanged
- **THEN** the encoded bytes are identical to before the feature.

### Requirement: Concurrency Safety

`AliveByQuality` and `Peer` RTT accessors SHALL be safe for concurrent use.

#### Scenario: concurrent ranking and heartbeat updates
- **GIVEN** concurrent calls to `AliveByQuality` and `Peer.SetRTT`
- **WHEN** all complete
- **THEN** no data race is observed under `-race` and ordering is internally
  consistent.
