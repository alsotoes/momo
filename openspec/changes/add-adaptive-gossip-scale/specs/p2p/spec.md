# Adaptive Gossip Fanout Scaling with Cluster Size

## Purpose

This specification makes gossip fanout adaptive to cluster size: by default a
node gossips to `ceil(ln N)` alive peers (bounded), instead of a fixed fanout,
preserving explicit `fanout > 0` configs as overrides.

## ADDED Requirements

### Requirement: Adaptive Fanout Computation

The gossip layer SHALL compute the default (adaptive) fanout for a given alive
peer count `N` as `clamp(ceil(ln N), minFanout, maxFanout)` with
`minFanout = 1` and `maxFanout = 10` (Rule 32).

#### Scenario: small cluster uses the minimum fanout
- **GIVEN** `N = 2` alive peers
- **WHEN** the adaptive fanout is computed
- **THEN** it equals `1` (`minFanout`).

#### Scenario: fanout grows but is bounded
- **GIVEN** `N = 100` alive peers
- **WHEN** the adaptive fanout is computed
- **THEN** it equals `maxFanout` and never exceeds `10`.

#### Scenario: monotonic scaling
- **GIVEN** cluster sizes `N1 < N2`
- **WHEN** adaptive fanout is computed for each
- **THEN** `fanout(N1) <= fanout(N2)`.

### Requirement: Config Semantics

`fanout = 0` (or unset) SHALL mean adaptive; `fanout > 0` SHALL be an explicit
fixed override.

#### Scenario: adaptive default
- **GIVEN** a config with `fanout` unset (defaults to `0`)
- **WHEN** the gossiper resolves fanout for a heartbeat
- **THEN** it uses `adaptiveFanout(aliveCount)`.

#### Scenario: explicit override preserved
- **GIVEN** a config with `fanout = 3`
- **WHEN** the gossiper resolves fanout for a heartbeat
- **THEN** it uses exactly `3`, regardless of alive count.

### Requirement: Resolution at Send Time

Fanout SHALL be resolved per heartbeat from the current alive peer count, so it
tracks cluster membership changes without a restart.

#### Scenario: fanout tracks membership growth
- **GIVEN** a node whose alive peer count grows from 3 to 30 after members join
- **WHEN** subsequent heartbeats are sent
- **THEN** the adaptive fanout used reflects the larger alive count.

### Requirement: Wire Stability

This change SHALL NOT alter heartbeat/membership RPC message types, payloads,
or byte layouts (Rules 7, 38).

#### Scenario: no protocol bytes change
- **GIVEN** no configuration change
- **WHEN** gossip heartbeats are exchanged
- **THEN** encoded message bytes are identical to before the feature.

### Requirement: Concurrency Safety

Fanout resolution SHALL read the peer map safely under concurrency.

#### Scenario: concurrent heartbeats and membership updates
- **GIVEN** concurrent `sendHeartbeat` calls and peer-map mutations
- **WHEN** all complete
- **THEN** no data race is observed under `-race`.
