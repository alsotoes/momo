# Denial Observability

## Purpose

Make refusals visible: a refused request must be as observable as a completed one,
in both the audit trail and the metrics endpoint.

## Requirements

### Requirement: Refusals are audited with a denied outcome

An authorization or quota refusal SHALL append an audit entry whose `outcome` is
`denied`, attributed to the authenticated tenant.

#### Scenario: Authorization denial

- **Given** a tenant with no ACL entry for the requested resource
- **When** the tenant sends the request
- **Then** the request is refused and an audit entry records `operation=Authorize, outcome=denied` for that tenant

#### Scenario: Quota refusal

- **Given** a tenant whose write would exceed its quota
- **When** the write is attempted
- **Then** the request is refused and an audit entry records `operation=PutObject, outcome=denied` for that tenant

### Requirement: Refusals are counted

The daemon SHALL export a counter for authorization denials and a counter for
quota refusals, incremented once per refused request.

#### Scenario: Counters move on refusal, not on success

- **Given** a running daemon with metrics enabled
- **When** one request is denied and another succeeds
- **Then** exactly one denial is counted and the success path leaves both counters unchanged

#### Scenario: Counters are declared at zero

- **Given** a daemon that has refused nothing
- **When** the metrics endpoint is scraped
- **Then** both counters appear with value zero

## Constraints

- Counting SHALL NOT require a metrics hook: communicators without one stay no-ops.
- Auditing SHALL NOT fail the request: an audit write error is logged only.
- Native-protocol requests SHALL NOT produce denial entries (no tenant, no ACL).
