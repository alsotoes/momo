# Configuration Honesty

## Purpose

Every accepted configuration key must either change behaviour or be rejected. The
loader is the last place that can tell an operator the truth about what the daemon
will do.

## Requirements

### Requirement: Configured metadata quorum is honoured

The metadata write and delete quorum SHALL be the configured
`[momofs] metadata_quorum` when set, clamped to the replica-set size, and a strict
majority of the replica set when unset.

#### Scenario: Explicit quorum

- **Given** `metadata_replication = 5` and `metadata_quorum = 2`
- **When** a metadata write is replicated
- **Then** two acknowledgements complete it instead of the majority (3)

#### Scenario: Over-large quorum is clamped

- **Given** a configured `metadata_quorum` larger than the replica set
- **When** a metadata write is replicated
- **Then** the required acknowledgements equal the replica-set size, so a
  misconfiguration cannot make every write fail

#### Scenario: Unset keeps the default

- **Given** no `metadata_quorum`
- **When** a metadata write is replicated to five replicas
- **Then** three acknowledgements are required

### Requirement: Unimplemented keys are refused at load

A key the daemon does not honour SHALL cause a load error that names the key, says
it is not implemented and links the tracking issue. Defaults SHALL keep loading.

#### Scenario: Refused keys

- **Given** `write_quorum = 2`, or an explicit `metadata_ttl`, or an explicit `lease_timeout`
- **When** the configuration is loaded
- **Then** loading fails with an error naming that key and the tracking issue

#### Scenario: Defaults still work

- **Given** a configuration that sets none of those keys
- **When** it is loaded
- **Then** it loads, with `write_quorum` 1, `metadata_ttl` 60s and `lease_timeout` 10

## Constraints

- The refusal SHALL be based on the key being present, not on its value, so an
  operator who writes the documented default gets the same honesty.
- `write_quorum = 1` SHALL be accepted: it describes what the daemon already does.
