# Tasks — Authenticate the Admin Listener

## Implementation
- [x] `[metrics] admin_token` in `struct.go` + parsing/validation (`>= 16` chars)
- [x] Bearer gate (constant-time) and loopback detection in `metrics_exporter.go`
- [x] `StartMetricsServer` takes the admin token; `/health` always open
- [x] Errors: `403` (no token off-loopback), `401` (missing/wrong token)
- [x] Pass the token from `server.go`

## Tests
- [x] Loopback without token: reload runs
- [x] Non-loopback without token: `403`, no reload
- [x] Token set: correct bearer succeeds, wrong/missing `401`
- [x] `/health` open in every combination
- [x] `/metrics` gated when a token is set
- [x] Config: default empty, short token rejected, valid token accepted

## Documentation
- [x] CONFIGURATION `[metrics]` section
- [x] ARCHITECTURE §7b residual risk updated to describe the new control
- [x] `conf/momo.conf`
- [x] ADR + `.no-blog.md` justification (hardening, no new engineering narrative)
