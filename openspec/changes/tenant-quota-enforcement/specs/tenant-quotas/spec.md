# Tenant Quotas

## Purpose

Bound the storage a single tenant can consume, so one tenant cannot exhaust a
node's disk or object count, while leaving single-tenant deployments unchanged.

## Requirements

### Requirement: Per-tenant usage is accounted

The store SHALL maintain, per tenant, the number of bytes and logical objects
charged to it, in a dedicated bucket, updated atomically with the limit check.

#### Scenario: Usage grows with a write

- **Given** a tenant with a byte quota
- **When** an object of size N is stored under that tenant
- **Then** the tenant's usage reports N bytes and one object

#### Scenario: Dedup does not change the accounting unit

- **Given** two different object names whose content is identical
- **When** both are stored under one tenant
- **Then** the tenant is charged two objects and twice the logical size

### Requirement: A write that would exceed a quota is refused before it lands

The shared ingest path SHALL reserve the delta before reading the payload and
SHALL refuse with a quota error when the reservation would exceed a configured
limit.

#### Scenario: Byte quota exceeded

- **Given** a tenant whose remaining byte quota is smaller than the upload
- **When** the tenant uploads the object
- **Then** the write fails, the object is absent, and usage is unchanged

#### Scenario: Object quota exceeded

- **Given** a tenant whose object count equals its quota
- **When** it stores one more object
- **Then** the write fails and usage is unchanged

#### Scenario: Exactly at the limit

- **Given** a remaining quota exactly equal to the upload size
- **When** the object is stored
- **Then** the write succeeds

### Requirement: Overwrites pay only the delta

Storing a new value under an existing name SHALL adjust bytes by the difference
and SHALL NOT increment the object count.

#### Scenario: Larger overwrite

- **Given** an object of 5 bytes already charged to the tenant
- **When** it is overwritten with 8 bytes
- **Then** the tenant is charged 8 bytes and still one object

### Requirement: Failed writes release their reservation

If the payload is rejected after the reservation (hash mismatch, checksum
mismatch, storage failure), the reservation SHALL be reversed.

#### Scenario: Hash mismatch

- **Given** a reserved upload
- **When** the received content does not match its declared hash
- **Then** the object is deleted and the tenant's usage is unchanged

### Requirement: Deletes release usage

A successful object delete SHALL charge the object's bytes and count back to the
tenant.

#### Scenario: Delete frees the quota

- **Given** a tenant charged for one object of size N
- **When** that object is deleted through the gateway
- **Then** the tenant's usage reports zero bytes and zero objects

### Requirement: Quotas are opt-in and surface-scoped

Enforcement SHALL be a no-op when both quotas are unset, when the tenant is
unknown or empty, and on surfaces that carry no tenant identity. The S3 gateway
SHALL map a refusal to `403 QuotaExceeded`.

#### Scenario: Single-tenant deployment

- **Given** a configuration with no `[tenant.<id>]` sections
- **When** any object is stored
- **Then** behaviour is identical to a build without quota enforcement

#### Scenario: Native protocol write

- **Given** a configured tenant with a quota
- **When** an object is stored over the native TCP/QUIC protocol (no tenant identity)
- **Then** no usage is charged and the write is not refused

## Constraints

- The check and the update SHALL occur in one transaction, so concurrent writes at
  the boundary never exceed the limit.
- Accounting SHALL count logical objects, not content-addressed blobs.
- A refused write SHALL NOT leave bytes on disk.
