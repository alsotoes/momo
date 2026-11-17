# Incomplete Multipart Upload Expiry

## Purpose

Bound the disk and memory an abandoned multipart upload can hold.

## Requirements

### Requirement: Expired Sessions Are Discarded

An incomplete multipart session older than the configured TTL SHALL be removed
along with its spooled parts.

#### Scenario: Abandoned upload

- **WHEN** a client uploads parts and never completes or aborts
- **AND** the session is older than `multipart_upload_ttl`
- **THEN** the session and its spill directory are removed, and an AUDIT line
  records the expiry

#### Scenario: In-flight upload

- **WHEN** a session is younger than the TTL
- **THEN** it is untouched, including its parts

### Requirement: Orphaned Spills Are Swept

A spill directory that no live session owns SHALL be removed once it is older
than the TTL.

#### Scenario: Daemon killed mid-upload

- **WHEN** a spill directory exists with no session behind it and is older than
  the TTL
- **THEN** it is removed at startup or on the next sweep

#### Scenario: Live session protection

- **WHEN** a spill directory belongs to a live session, however old
- **THEN** the sweep leaves it alone

### Requirement: Disableable

`multipart_upload_ttl = 0` SHALL disable reaping, and a negative or unparseable
value SHALL be refused at config load.

#### Scenario: Reaping disabled

- **WHEN** the TTL is `0`
- **THEN** no session or directory is removed and no loop is started
