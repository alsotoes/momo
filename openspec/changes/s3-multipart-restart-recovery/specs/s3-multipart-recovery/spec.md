# Multipart Restart Recovery

## Purpose

Survive a daemon restart without discarding an in-flight multipart upload.

## Requirements

### Requirement: Sessions Are Durable

An open upload SHALL record its identity and parts in its spill directory.

#### Scenario: Created upload

- **WHEN** `CreateMultipartUpload` succeeds
- **THEN** a manifest exists for it before any part arrives

#### Scenario: Part registered

- **WHEN** a part is accepted
- **THEN** the manifest lists it, and it is written only after the part file is in
  place

#### Scenario: Crash mid-write

- **WHEN** the process dies while the manifest is being written
- **THEN** the previous manifest is intact, because the write is a rename

### Requirement: Sessions Are Recovered

The gateway SHALL rebuild its session map from manifests at startup, keeping upload
ids stable.

#### Scenario: Restart during an upload

- **WHEN** the daemon restarts while an upload has parts
- **THEN** the same `uploadId` can `ListParts` and `CompleteMultipartUpload`, and the
  assembled object matches the uploaded bytes

### Requirement: Recovery Is Conservative

#### Scenario: Missing or truncated part

- **WHEN** the manifest lists a part whose file is missing or shorter than recorded
- **THEN** the session is not recovered, so it cannot complete with wrong bytes

#### Scenario: Malformed manifest

- **WHEN** a manifest cannot be parsed or lacks bucket/key/upload id
- **THEN** it is ignored and recovery continues

#### Scenario: Expired session

- **WHEN** a session is older than `multipart_upload_ttl`
- **THEN** it is not recovered, leaving expiry as the single policy that reclaims it

#### Scenario: Two owners

- **WHEN** a live session already holds an id that a manifest also claims
- **THEN** the live one wins
