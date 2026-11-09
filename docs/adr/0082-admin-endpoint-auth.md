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
- Mutating endpoints are never unauthenticated off-loopback: `POST /reload-secrets` SHALL require either a valid bearer token or a loopback bind. - **Given** the metrics listener bound to `127.0.0.1` with no `admin_token` - **When** an operator posts to `/reload-secrets` - **Then** the reload runs and the response is `200` - **Given** the listener bound to `0.0.0.0` (or a routable address) with no `admin_token` - **When** any client posts to `/reload-secrets` - **Then** the response is `403` with a message naming `[metrics] admin_token` - **And** no secre...
- Liveness is always open: `GET /health` SHALL remain reachable without credentials on every bind. - **Given** any bind and any token configuration - **When** an orchestrator probes `/health` - **Then** the response is `200`
- Token comparison is constant time: Bearer comparison SHALL use a constant-time comparison and SHALL NOT log the presented value. - **Given** a configured token - **When** a client sends a different token of the same length - **Then** the response is `401` and the request is not executed

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

