# 0082-admin-endpoint-auth

## Status
Accepted

## Confidence
High

## Context
`StartMetricsServer` serves `/metrics`, `/health`, `/health/detailed` and
`POST /reload-secrets` with no authentication, and binds all interfaces by
default. Three of those are read-only, but `/reload-secrets` **reloads secrets and
advances the key registry** — an unauthenticated administrative mutation reachable
from the network. It has been documented as residual risk; documenting is not
fixing.

## Decision
- Mutating endpoints are never unauthenticated off-loopback: `POST /reload-secrets` SHALL require either a valid bearer token or a loopback bind.
- Liveness is always open: `GET /health` SHALL remain reachable without credentials on every bind.
- Token comparison is constant time: Bearer comparison SHALL use a constant-time comparison and SHALL NOT log the presented value.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: 

## References
- Issue: #1289
- PR: 
- Spec: `openspec/changes/admin-endpoint-auth/`
- Blog: 

