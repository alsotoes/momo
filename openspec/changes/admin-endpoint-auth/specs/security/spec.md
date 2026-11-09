# Admin Listener Authentication

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1289

### Requirement: Mutating endpoints are never unauthenticated off-loopback

`POST /reload-secrets` SHALL require either a valid bearer token or a loopback bind.

#### Scenario: Loopback without a token

- **Given** the metrics listener bound to `127.0.0.1` with no `admin_token`
- **When** an operator posts to `/reload-secrets`
- **Then** the reload runs and the response is `200`

#### Scenario: Non-loopback without a token

- **Given** the listener bound to `0.0.0.0` (or a routable address) with no `admin_token`
- **When** any client posts to `/reload-secrets`
- **Then** the response is `403` with a message naming `[metrics] admin_token`
- **And** no secret reload is performed

#### Scenario: Token configured

- **Given** `[metrics] admin_token` is set
- **When** a client presents the correct bearer token
- **Then** read endpoints and `/reload-secrets` succeed
- **When** the token is missing or wrong
- **Then** the response is `401` and nothing is executed

### Requirement: Liveness is always open

`GET /health` SHALL remain reachable without credentials on every bind.

#### Scenario: Health without a token

- **Given** any bind and any token configuration
- **When** an orchestrator probes `/health`
- **Then** the response is `200`

### Requirement: Token comparison is constant time

Bearer comparison SHALL use a constant-time comparison and SHALL NOT log the
presented value.

#### Scenario: Wrong token of equal length

- **Given** a configured token
- **When** a client sends a different token of the same length
- **Then** the response is `401` and the request is not executed
