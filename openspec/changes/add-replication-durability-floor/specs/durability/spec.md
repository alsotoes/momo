# Replication Durability Floor Under Load

## Purpose

This specification prevents silent durability loss by ensuring the
metrics-driven controller never automatically selects a replication mode whose
achievable replica count is below an operator-configured minimum durability
floor, holding the current higher-durability mode (and logging) instead of
degrading.

## ADDED Requirements

### Requirement: Configurable Minimum Durability Factor

The global configuration SHALL expose a `minimum_durability_factor` setting
(default `0`, meaning disabled). When `N >= 1`, the metrics controller SHALL
NOT select an active mode whose achievable replica count is below `N`.

#### Scenario: default floor is disabled
- **GIVEN** a configuration with no `minimum_durability_factor`
- **WHEN** the metrics controller runs
- **THEN** it behaves exactly as before the feature, with no new constraints.

#### Scenario: an explicit floor is honored by mode selection
- **GIVEN** `minimum_durability_factor = 2` and a cluster of 3 daemons with
  `replication_factor = 3`
- **WHEN** the metrics controller would select `ReplicationNone`
- **THEN** it keeps the current higher-durability mode instead of degrading and
  logs a warning that the floor would be violated.

### Requirement: Achievable Replica Count Computation

The controller SHALL compute a mode's achievable replica count as
`min(replication_factor, number_of_daemons)` for replicated modes
(`ReplicationChain`, `ReplicationSplay`, `ReplicationPrimarySplay`) and `1` for
`ReplicationNone`.

#### Scenario: replicated mode bounded by cluster size
- **GIVEN** `replication_factor = 5` but only 2 daemons
- **WHEN** the achievable replica count for `ReplicationChain` is computed
- **THEN** it equals `2` (min(5, 2)), not `5`.

#### Scenario: none mode has a single copy
- **GIVEN** any configuration
- **WHEN** the achievable replica count for `ReplicationNone` is computed
- **THEN** it equals `1`.

### Requirement: Degrade is Held at the Floor

In `GetMetrics`, when `checkMetricsAndSwap` or the timeout fallback proposes a
mode with achievable replica count below the configured minimum, the controller
SHALL NOT switch to it and SHALL log the refusal so the operation is not silent
(Rule 9).

#### Scenario: load-driven degrade below floor is refused
- **GIVEN** `minimum_durability_factor = 2`, current mode `ReplicationSplay`,
  and metrics pressure that would propose moving toward `ReplicationNone`
- **WHEN** the controller evaluates the degrade
- **THEN** the mode is unchanged and a warning is logged.

#### Scenario: escalation is never blocked by the floor
- **GIVEN** a configured minimum durability floor
- **WHEN** the controller proposes raising durability (moving to a mode with
  more replicas)
- **THEN** the escalation proceeds normally, unaffected by the floor.

### Requirement: Operator Control Channel Unaffected

The floor SHALL apply only to the automatic metrics-driven degrade path, not to
operator-driven mode changes via the change-replication control channel.

#### Scenario: operator can still set a mode above or below the floor
- **GIVEN** a configured minimum durability floor
- **WHEN** an operator changes the replication mode through the control channel
- **THEN** the change is accepted as today; the floor does not veto it.

### Requirement: Configuration Validation

Configuration parsing SHALL reject an invalid `minimum_durability_factor`
(negative, or greater than the configured `replication_factor` when the floor is
enabled) with `EINVAL`.

#### Scenario: invalid floor is rejected at boot
- **GIVEN** `minimum_durability_factor = 10` while `replication_factor = 3`
- **WHEN** the configuration is loaded
- **THEN** loading fails with a clear `EINVAL` error.
