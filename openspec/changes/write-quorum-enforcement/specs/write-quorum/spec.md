# Write Quorum

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1340

## Purpose

Make `write_quorum` a real durability control: a write is acknowledged to the
client only after the configured number of replicas have acknowledged it.

## Requirements

### Requirement: Acknowledged Replica Counting

The daemon SHALL count the local store as one acknowledged replica and SHALL
count each replication forwarder that completed without error as one additional
acknowledged replica, because such a forwarder has already received the peer's
ACK.

#### Scenario: Chain forward succeeds

- **WHEN** a write is stored locally and the chain forwarder returns success
- **THEN** the acknowledged replica count is 2

#### Scenario: Forwarder fails

- **WHEN** a forwarding goroutine returns an error
- **THEN** it contributes no acknowledged replica and the write is not acknowledged

### Requirement: Quorum Enforcement

When `write_quorum` is greater than 1 the daemon SHALL withhold the success ACK
if the acknowledged replica count is below the configured quorum, so the client
observes a failed write.

#### Scenario: Quorum unmet

- **WHEN** `write_quorum` is 3 and only 2 replicas acknowledged (chain mode)
- **THEN** the server sends no ACK and records an AUDIT line plus a replication failure

#### Scenario: Quorum met

- **WHEN** the acknowledged replica count is at least `write_quorum`
- **THEN** the server sends the success ACK as usual

### Requirement: Configuration Validity

The daemon SHALL accept `write_quorum` in `[1, replication_factor]` and SHALL
refuse at load a value above 1 unless `replication_order` contains a
server-side forwarding mode (chain or splay).

#### Scenario: Client-side-only order

- **WHEN** `replication_order` contains only client-side modes and `write_quorum` is 2
- **THEN** configuration loading fails with an error naming the missing forwarding mode
