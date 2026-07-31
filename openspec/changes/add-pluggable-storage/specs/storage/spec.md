## ADDED Requirements

### Requirement: Pluggable Storage Backend
The system SHALL support configurable storage backends selected via the `backend` config field in the `[storage]` section. The default backend (`local`) SHALL preserve existing behavior exactly.

#### Scenario: Default Backend (Unconfigured)
- **GIVEN** the `[storage]` section has no `backend` field or `backend = ""`
- **WHEN** the server starts
- **THEN** the system SHALL use the `local` backend (local filesystem with tiered directory layout).

#### Scenario: Local Backend
- **GIVEN** `backend = "local"` in the `[storage]` section
- **WHEN** the server starts
- **THEN** the system SHALL store blobs on the local filesystem at `daemon.data`.

#### Scenario: NFS Backend
- **GIVEN** `backend = "nfs"` and `daemon.data` is an NFS mount path
- **WHEN** the server starts
- **THEN** the system SHALL store blobs on the NFS mount via the same local filesystem logic.

#### Scenario: S3 Backend
- **GIVEN** `backend = "s3"` with valid `s3_endpoint`, `s3_bucket`, `s3_access_key`, `s3_secret_key`
- **WHEN** the server starts
- **THEN** the system SHALL store blobs in the S3-compatible bucket, with bbolt metadata remaining local.

#### Scenario: Raw Device Backend
- **GIVEN** `backend = "raw"` with `raw_device_path` or `daemon.drive` set
- **WHEN** the server starts
- **THEN** the system SHALL store blobs via direct block I/O on the device, with allocation metadata in local bbolt.

## MODIFIED Requirements

### Requirement: Content-Based File Addressing
The system SHALL store and retrieve files based on a cryptographic hash of their content, using the pluggable `BlobStore` interface for raw blob bytes and local bbolt for metadata.

#### Scenario: Replication Forwarding Without Local File Path
- **GIVEN** a node has received and stored a blob via any backend
- **WHEN** the node forwards the blob to a peer for Chain or Splay replication
- **THEN** the system SHALL stream the blob via `store.Get()` → `connectToPeerStream(io.Reader)`, without requiring a local filesystem path.
