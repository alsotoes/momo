> GitHub Issue URL: https://github.com/alsotoes/momo/issues/935

# Spec: R7 — Error Model + Ops (ENOSPC, Exit Codes, Cluster Health)

## Requirements

### R7-E1: ENOSPC Signaling on Disk Full

**Requirement:** All storage write paths MUST return `syscall.ENOSPC` when the underlying filesystem reports no space left.

**Scenario: Disk full during S3 PUT**

Given a node's storage disk is at 100% capacity,
When an S3 client issues a `PUT` request,
Then the handler returns `507 Insufficient Storage` with error code `ENOSPC`,
And the response body includes the standard S3 error XML.

**Scenario: Disk full during native protocol upload**

Given a node's storage disk is at 100% capacity,
When a native TCP/QUIC client uploads a blob,
Then the server responds with `ENOSPC` error code,
And the connection is cleanly closed.

### R7-E2: Distinct Exit Codes

**Requirement:** The process MUST exit with distinct codes for different failure classes.

**Scenario: Configuration error on startup**

Given `momo.conf` is missing a required key (e.g., `auth_token`),
When `momo -imp server` starts,
Then the process exits with code `2` and logs the specific missing key.

**Scenario: Port bind failure**

Given the configured `tcp_port` is already in use,
When `momo -imp server` starts,
Then the process exits with code `3` and logs the bind error.

**Scenario: Disk full during runtime**

Given a write path encounters `ENOSPC` during blob storage,
When the server attempts to write,
Then the process exits with code `4` (if fatal) or returns `ENOSPC` to client.

### R7-E3: Cluster Health Endpoint

**Requirement:** A `/health/detailed` HTTP endpoint MUST expose full cluster state.

**Scenario: Operator queries cluster health**

Given the server is running,
When `GET /health/detailed` is requested,
Then the response is JSON with:
- `node_id`, `uptime_seconds`, `state` (healthy/degraded/critical)
- `disk_used_bytes`, `disk_free_bytes`, `disk_percent_used`
- `peers_alive`, `peers_suspect`, `peers_offline`
- `replication_mode`, `pending_replication_count`
- `scrub_status` (running/idle, last_run_ts, error_count)
- `leases_active`, `scatter_queries_pending`

**Scenario: Health endpoint reflects degraded state**

Given a node has a suspect peer or disk > 90% used,
When `/health/detailed` is queried,
Then `state` is `degraded` and the relevant fields are populated.

## Non-Goals

- OpenTelemetry / distributed tracing
- Prometheus alerting rules (separate config artifact)
- Structured logging format changes