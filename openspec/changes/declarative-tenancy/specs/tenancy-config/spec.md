# Declarative Tenancy

## Purpose

Make multi-tenancy usable from configuration alone: a deployment declares its
tenants and bucket permissions, and the daemon makes them real before it accepts
a request.

## Requirements

### Requirement: Tenants declared in configuration authenticate

Every `[tenant.<id>]` entry SHALL be persisted to the tenant store at startup, so
its `auth_token` resolves during the S3 handshake.

#### Scenario: Declared tenant authenticates

- **Given** `[tenant.acme]` with an `auth_token`
- **When** the daemon starts and a client presents that token
- **Then** the handshake binds the request to tenant `acme`

#### Scenario: Undeclared token is refused

- **Given** a store with no such tenant
- **When** a client presents an unknown token
- **Then** the request is refused (unchanged behaviour)

### Requirement: Bucket ACLs are declarable

`[acl.<bucket>]` sections SHALL populate bucket ACLs: each key is a tenant ID and
each value a comma-separated permission list, where a permission prefixed with `!`
is stored as an explicit deny.

#### Scenario: Configured permission is enforced

- **Given** `[acl.photos]` granting `acme = read`
- **When** tenant `acme` reads an object in `photos`
- **Then** authorization passes and the request reaches dispatch

#### Scenario: Unconfigured bucket stays default-deny

- **Given** no `[acl.docs]` section
- **When** tenant `acme` reads from `docs`
- **Then** the request is refused with `403 AccessDenied`

#### Scenario: Explicit deny from configuration

- **Given** `beta = read,!delete`
- **When** tenant `beta` deletes from that bucket
- **Then** the stored entry has effect `deny` and the request is refused

### Requirement: Bootstrap is idempotent and conservative

Seeding SHALL be safe to repeat and SHALL NOT remove state it did not declare.

#### Scenario: Restart

- **Given** a daemon that already seeded its tenants and ACLs
- **When** it restarts with the same configuration
- **Then** no duplicate entries appear and no error occurs

#### Scenario: Removed entry is revoked

- **Given** configuration that no longer declares a tenant that is in the store
- **When** the daemon starts, and the section still declares another tenant
- **Then** the removed tenant is deleted, so its token stops working

#### Scenario: Absent sections never wipe

- **Given** a daemon whose configuration declares no tenants and no ACLs
- **When** it starts
- **Then** stored tenants and ACLs are left untouched

### Requirement: Invalid declarations fail at load

An ACL declaration SHALL be rejected when it names an unknown permission, an
invalid bucket, an empty tenant key or an empty permission list.

#### Scenario: Typo in a permission

- **Given** `acme = read,reed`
- **When** the configuration is loaded
- **Then** loading fails naming `reed`, so a typo cannot silently widen access

## Constraints

- Bootstrap SHALL run before the listener accepts connections.
- A bootstrap error SHALL abort startup.
- Object-level ACLs remain API-only (no declarative surface yet).
