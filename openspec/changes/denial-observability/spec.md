
### Requirement: Denial audit records are rate-limited

A denial is induced by the client, so denial audit records SHALL be bounded: an
identical `(tenant, operation, resource)` denial SHALL be recorded at most once per
window, and no more than a fixed number of denial records SHALL be written per
second regardless of resource variety. Refusal counters SHALL remain exact.

#### Scenario: Repeated identical denial

- **Given** a tenant repeatedly denied access to the same resource
- **When** twenty requests are refused inside the window
- **Then** one denial audit entry is written and the denial counter increases by twenty

#### Scenario: Distinct-resource flood

- **Given** a client using a different key on every request
- **When** it floods the gateway with denied requests
- **Then** at most the per-second cap of denial audit entries is written

#### Scenario: Successful operations are never suppressed

- **Given** a tenant with access
- **When** it performs twenty successful operations
- **Then** twenty audit entries are written

## Constraints (rate limiting)

- Suppression state SHALL be in-memory and fixed-capacity, so it cannot become a leak.
- Suppression SHALL be visible in the daemon log, one line per hundred suppressions.
- Successful-operation auditing SHALL NOT be rate-limited.
