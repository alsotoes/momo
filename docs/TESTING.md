# Testing & CI Pipeline

This document describes every test suite and validation step that runs in the Momo CI/CD pipeline.

## Pipeline Overview

| Workflow | File | Trigger | Purpose |
|---|---|---|---|
| Go | `go.yml` | push to master, PRs | Build, unit tests, benchmarks, E2E, coverage, scanner-safe secrets |
| Smoke Test | `smoke_test.yml` | push to master, PRs | Multi-protocol file replication verification |
| Scale & CAS E2E | `scale_cas_test.yml` | push to master, PRs (path-filtered) | CAS storage scale testing |
| P2P Gossip E2E | `p2p_test.yml` | push to master, PRs (path-filtered) | P2P gossip convergence + failure detection |
| Distributed Testing | `distributed_test.yml` | push to master, PRs (path-filtered) | TCP contract, k6 load, chaos node-crash |
| Metrics Test | `metrics_test.yml` | push to master, PRs | Prometheus metrics endpoint format + counter verification |
| Performance Comparison | `benchmark_compare.yml` | PRs, push to master | Benchmark regression detection (>5% threshold) |
| Go Version Consistency | `verify_go_version.yml` | PRs, push to master | Go version sync across all config files |
| Gemini AI Reviewer | `gemini_reviewer.yml` | PRs | AI code review (security, performance, architecture) |
| Auto Reviewer | `auto_reviewer.yml` | PR opened/reopened | Initial automated review |
| Weekly Sanity | `weekly_sanity.yml` | Weekly cron (Sun 00:00 UTC) | Full suite + security audit |
| Storage Backend E2E | `storage_backends_test.yml` | PRs touching `src/storage/` | S3 and raw device backend E2E tests (`-race`) |
| External Client Replication | `external_client_test.yml` | push to master, PRs (path-filtered) | External S3 client replication mode downgrade |
| Encryption Smoke | `encryption_smoke_test.yml` | push to master, PRs | E2E encryption across TCP/QUIC/S3 transports (`make smoke-encryption-*`) |
| MomoFS FUSE E2E | `momofs_fuse_test.yml` | push to master, PRs (path-filtered) | FUSE mount round-trip (`TestFuseE2E_MountRoundTrip`) |
| Blog Check | `blog_check.yml` | PRs, push to master | Validates docs/blog posts (Rule 76) |
| Pentest | `pentest.yml` | push to master, PRs, manual dispatch | DotDotPwn fuzzing + Python exploit toolkit (10 CVEs found) |

---

## Go Workflow (`go.yml`)

The primary CI pipeline. Runs on every push to `master` and every PR targeting `master`.

### Steps

| Step | Command | What it tests |
|---|---|---|
| **Build** | `make build` | Compiles the momo binary |
| **Check Formatting** | `make fmt` + git diff check | All `.go` files are `gofmt`-compliant (Rule 26) |
| **Check Vendoring** | `make vendor` + git diff check | `vendor/` directory is in sync with `go.work` (Rule 25) |
| **Test** | `make test` | `go test -v -race -cover` across all modules |
| **Benchmark** | `make benchmark` | `go test -bench=. -benchmem` across all modules |
| **E2E Integration Tests** | `make test-e2e` | 3-node cluster: file upload + replication consistency |
| **Coverage** | `make coverage` | Generates HTML coverage report |
| **Upload Coverage** | `upload-artifact` | Stores `coverage.out` as CI artifact |
| **Check Scanner-Safe Secrets** | `check-notsecret.sh` | Rule 29: all dummy tokens annotated with `notsecret` |

### Modules under test

```
./src/common ./src/transport ./src/client ./src/metrics ./src/p2p ./src/server ./src/storage ./src/crypto ./src/momofs
```

### Test flags

- `-race`: Race detector enabled (Rule 5)
- `-cover`: Coverage profiling
- `-v`: Verbose output

---

## Smoke Test Workflow (`smoke_test.yml`)

Runs 4 parallel jobs, each testing a different wire protocol.

| Job | Command | Protocol tested |
|---|---|---|
| Smoke Test (TCP) | `make smoke-tcp` | `momo-tcp` |
| Smoke Test (QUIC) | `make smoke-quic` | `momo-quic` |
| Smoke Test (S3-TCP) | `make smoke-s3-tcp` | `s3-tcp` |
| Smoke Test (S3-QUIC) | `make smoke-s3-quic` | `s3-quic` |

Each smoke test:
1. Builds the momo binary
2. Starts 3 server daemons
3. Uploads a test file via the client
4. Verifies the file is replicated to all 3 nodes

---

## Scale & CAS E2E Test (`scale_cas_test.yml`)

Runs `.github/scripts/test-scale-cas.sh` — exercises the CAS (Content-Addressable Storage) engine at scale, verifying CRUSH placement, deduplication, and metadata consistency.

---

## P2P Gossip E2E Test (`p2p_test.yml`)

Runs `.github/scripts/test-e2e-p2p.sh` — tests P2P gossip membership and failure detection across 3 separate momo server processes.

1. Builds momo binary
2. Creates a 3-daemon config with `[p2p] enabled=true`
3. Starts 3 server processes with P2P gossip enabled
4. Waits for gossip convergence (8 seconds)
5. Verifies all nodes started P2P gossip
6. Verifies nodes discovered each other via gossip heartbeats
7. **Kills node 2** to simulate failure
8. Waits for suspicion timeout (12 seconds)
9. Verifies surviving nodes marked the dead node as SUSPECT or OFFLINE

---

## Performance Comparison (`benchmark_compare.yml`)

Runs on PRs and pushes to `master`.

1. Runs `make benchmark COUNT=15` on the current commit
2. Runs the same benchmarks on the base commit (PR base or `HEAD^1`)
3. Compares results with `benchstat`
4. **Fails if any benchmark degrades by more than 5%** (excluding known noisy micro-benchmarks)

---

## Go Version Consistency (`verify_go_version.yml`)

Ensures the Go version is synchronized across:
- Root `go.mod` (source of truth)
- All `src/*/go.mod` files
- All `.github/workflows/*.yml` files
- `Dockerfile`
- `.idx/dev.nix`

---

## Weekly Sanity Check (`weekly_sanity.yml`)

Runs every Sunday at 00:00 UTC. Also manually triggerable via `workflow_dispatch`.

| Step | What it does |
|---|---|
| Build | `make build` |
| Format check | `make fmt` + git diff |
| Vendoring check | `make vendor` + git diff |
| Test suite | `make test` (with `-race` and leak checks) |
| Benchmarks | `make benchmark` |

---

## E2E Test Details

### Standard E2E (`test-e2e.sh`)

Tests file replication across 3 separate momo server processes.

1. Builds momo binary
2. Creates a 3-daemon config (momo-tcp by default)
3. Starts 3 server processes
4. Triggers replication mode change to Chain (mode 1)
5. Uploads a test file via the client
6. Verifies the file content exists on all 3 nodes

**Protocols tested:** `momo-tcp`, `momo-quic`, `s3-tcp`, `s3-quic`

---

## P2P Unit Tests (`src/p2p/`)

Run as part of `make test` in the Go workflow.

| Test | What it verifies |
|---|---|
| `TestRPC_EncodeDecode` | Binary RPC frame roundtrip |
| `TestRPC_EmptyPayload` | Edge case: nil payload |
| `TestHeartbeatPayload_EncodeDecode` | Heartbeat peer list binary roundtrip |
| `TestHeartbeatPayload_Empty` | Edge case: empty peer list |
| `TestPeer_StateTransitions` | Peer state: alive → suspect → offline |
| `TestPeer_Touch` | LastSeen timestamp updates |
| `TestPeerMap_AddGetRemove` | PeerMap basic operations |
| `TestPeerMap_All` | PeerMap snapshot |
| `TestPeerMap_Alive` | PeerMap filtering by state |
| `TestPeerMap_RandomPeers` | Gossip fanout selection with exclusion |
| `TestPeerMap_PeerInfos` | PeerInfo serialization prep |
| `TestPeerMap_ConcurrentAccess` | Thread safety under 100 concurrent writers |
| `TestTCPTransport_ListenDial` | TCP listen + dial |
| `TestTCPTransport_SendReceive` | RPC send/receive between 2 nodes |
| `TestTCPTransport_Broadcast` | RPC broadcast to 2 peers |
| `TestGossiper_HeartbeatExchange` | 2-node heartbeat exchange |
| `TestGossiper_MembershipDissemination` | 3-node membership propagation via gossip |
| `TestGossiper_SuspicionTimeout` | Peer marked suspect/offline after timeout |
| `TestIntegration_ThreeNodeCluster` | 3-node cluster: all nodes discover each other |
| `TestIntegration_NodeJoinAfterStart` | Node joins after cluster is running, discovered via gossip |
| `TestGossiper_PingAck` | Direct ping/ack between 2 nodes |
| `TestGossiper_IndirectPing` | Indirect ping via intermediary peer |
| `TestAdaptiveTimeout_Fallback` / `TestAdaptiveTimeout_WithRTT` | Adaptive timeout adjusts based on RTT |
| `TestAdaptiveTimeout_CappedAtMin` / `TestAdaptiveTimeout_CappedAtMax` | Adaptive timeout stays within min/max bounds |
| `TestRTTTracker_UpdateAndGet` | RTT EWMA tracking (alpha=0.25) |
| `TestRTTTracker_EWMAConvergence` | RTT exponential weighted moving average |
| `TestLeaseManager_AcquireRelease` | Lease acquire and release lifecycle |
| `TestLeaseManager_NoPeers` | Lease acquisition fails with no peers |
| `TestLeaseManager_Expiry` | Lease expires after timeout |
| `TestLeaseManager_QuorumTimeout` | Lease fails when quorum not reached in time |
| `TestScatterGather_Query` | Scatter-gather query across 3 nodes |
| `TestScatterGather_LargeData` | Scatter-gather with large response data |
| `TestQueryPayload_EncodeDecode` | Query payload binary roundtrip |
| `TestQueryResponsePayload_EncodeDecode` | Query response payload binary roundtrip |
| `TestLeasePayload_EncodeDecode` | Lease payload binary roundtrip |

### P2P Benchmarks

| Benchmark | ns/op | allocs/op |
|---|---|---|
| `BenchmarkRPC_Encode` | ~40 | 1 |
| `BenchmarkHeartbeatPayload_Encode` | ~86 | 1 |
| `BenchmarkPeerMap_RandomPeers` | ~2428 | 2 |
| `BenchmarkPeerMap_PeerInfos` | ~1838 | 1 |

---

## Running Tests Locally

```bash
# All unit tests with race detector
make test

# Benchmarks
make benchmark

# E2E tests (default: momo-tcp)
make test-e2e

# P2P gossip E2E
make test-e2e-p2p

# Smoke tests per protocol
make smoke-tcp
make smoke-quic
make smoke-s3-tcp
make smoke-s3-quic

# TCP contract tests
make test-contract

# Prometheus metrics E2E test
make test-metrics

# k6 load/stress tests (requires server running)
make test-load
make test-stress
make test-chaos

# Monitoring stack (Grafana + Prometheus)
make monitoring-up
make monitoring-down

# Coverage report
make coverage

# Format check
make fmt

# Vendor sync
make vendor

# Install git pre-commit hook
make install-hooks

# Security pentest (DotDotPwn + Python exploits)
make pentest

# Clean build artifacts
make clean

# Generate godoc HTML
make doc

# Sync go workspace
make tidy
```

---

## Pentest Workflow (`pentest.yml`)

Runs DotDotPwn fuzzing and Python exploit scripts against Momo's S3 REST gateway and native TCP wire protocol. Triggers on push/PR to master (path-filtered to `src/**/*.go`, `pentest/**`, etc.) and manual dispatch.

### Pipeline Phases (6 phases, ~25 steps)

| Phase | What it does |
|---|---|
| 1. Build | Set up Go 1.25 / Perl / Python 3, clone DotDotPwn, build Momo binary |
| 2. S3 fuzzing | Start S3 server, run DotDotPwn 4x (5526 patterns each), stop |
| 3. Native fuzzing | Start TCP server, pipe DotDotPwn patterns into Momo wire protocol via Python bridge |
| 4. Exploitation | Run Python exploit toolkit (8 CVEs), stop native server |
| 5. SigV4 bypass | Start S3 on port 8083, test invalid signature GET + PUT, stop |
| 6. Reports | Collect artifacts, upload, generate summary, security gates |

### Security Gates

Security gates **only fail on `workflow_dispatch`** (manual trigger), not on PR/push. This allows the pentest tooling to merge while keeping the gates as **regression checks** for previously-fixed CVEs (e.g. CVE-008 SigV4 bypass, fixed in PR #549).

| Gate | Condition | Failure means |
|---|---|---|
| SigV4 bypass | `SIGV4_BYPASS=true` | CVE-008 regression confirmed — S3 gateway accepts invalid signatures (fixed in PR #549) |
| Filesystem traversal | `RUN_B_ROOT != 0` | /etc/passwd content leaked via S3 gateway |

### Findings (10 CVEs)

| CVE | Vulnerability | Severity | Issue | Status |
|---|---|---|---|---|
| CVE-008 | S3 SigV4 signature bypass | Critical | #539 | ✅ Fixed (PR #549) |
| CVE-010 | SigV4 replay attack (no timestamp freshness) | High | #595 | ✅ Fixed (PR #722) |
| CVE-005 | Deduplication confusion attack | High | #540 | ✅ Fixed (PR #552) |
| CVE-007 | Peer impersonation | High | #541 | ✅ Fixed (PR #553) |
| CVE-002 | Arbitrary file download (GET) | High | #542 | ✅ Fixed (PR #554) |
| CVE-003 | Arbitrary file deletion (DELETE) | High | #543 | ✅ Fixed (PR #557) |
| CVE-001 | Arbitrary file enumeration (LIST) | Medium | #544 | ✅ Fixed (PR #562) |
| CVE-006 | Blob pollution / disk waste | Medium | #545 | ✅ Fixed (PR #563) |
| CVE-009 | Plaintext auth token (no TLS) | Medium | #546 | 🔒 Open — requires E2EE (#152) |
| CVE-004 | Virtual path traversal via upload | Low | #547 | ✅ Fixed |

**Run:** `make pentest` or see [pentest/README.md](../pentest/README.md) for full reproduction guide.

---

## Distributed Testing (`tests/`)

The `tests/` directory contains all distributed systems testing infrastructure: k6 load generation, chaos engineering scripts, monitoring stack, and scalability manifests. These complement the Go-level unit and integration tests by exercising the cluster as a whole.

### Directory Structure

```
tests/
├── k6/                         # k6 load/stress/chaos test scripts
│   ├── load_test.js            # Basic PUT load with ramping VUs
│   ├── stress_test.js          # High concurrency + Slowloris trickle
│   ├── chaos_test.js           # Upload → verify replication → delete → verify tombstone
│   └── k6.json                 # k6 run configuration metadata
├── chaos/                      # Chaos engineering shell scripts
│   ├── network_partition.sh    # Jepsen-style netsplit via iptables
│   ├── node_crash.sh           # Node kill -9 during replication
│   └── slow_network.sh         # Network degradation via tc/netem
├── monitoring/                 # Grafana + Prometheus observability stack
│   ├── docker-compose-monitoring.yml
│   ├── prometheus/
│   │   ├── prometheus.yml      # Scrape config (5s interval, 3 momo nodes)
│   │   └── alerts.yml          # Alert rules (error rate, node down, goroutines, memory)
│   └── grafana/
│       ├── provisioning.yml    # Auto-provisioned Prometheus datasource
│       └── dashboards/
│           └── momo-overview.json  # 7-panel dashboard
└── kubernetes/                 # Scalability testing manifests
    ├── momo-statefulset.yaml   # K8s StatefulSet (3 replicas, PVCs, health probes)
    ├── scale-test.yaml         # K8s Deployment running k6 load generator
    └── docker-swarm.yml        # Docker Swarm stack (3 nodes + 3 k6 workers)
```

---

### k6 Load Testing (`tests/k6/`)

Uses [k6](https://k6.io) for HTTP-based load generation against the Momo S3 API. Requires a running Momo cluster with `s3-tcp` or `s3-quic` protocol.

#### `load_test.js` — Basic Load Test

Ramping PUT workload against a single Momo server. Used in CI (`distributed_test.yml`).

| Parameter | Value |
|---|---|
| VUs | 10 → 20 → 0 (ramping) |
| Duration | ~50s |
| Payload | 16 KB random data per request |
| Endpoint | `PUT /{bucket}/{key}` |
| Thresholds | `http_req_failed < 5%`, `p(95) < 10s`, `put_errors < 100` |

**Environment variables:** `MOMO_URL` (default `http://localhost:3333`), `AUTH_TOKEN` (default `secret`), `BUCKET` (default `test-bucket`)

**Run:** `make test-load` or `k6 run tests/k6/load_test.js`

#### `stress_test.js` — High Concurrency + Slowloris

Two concurrent scenarios simulating production stress conditions:

| Scenario | VUs | Duration | Payload | Description |
|---|---|---|---|---|
| `high_throughput` | 200 → 500 → 1000 → 0 | ~2.5 min | 256 KB | High-volume PUT uploads with 15s timeout |
| `slowloris_trickle` | 50 → 200 → 0 | ~1.7 min | 16 KB | Slow clients with 30s timeout, accepts 200/408/429 |

**Thresholds:** `failed_uploads < 100`, `healthy_connection_rate > 95%`, `p(99) < 15s`

**Run:** `make test-stress` or `k6 run tests/k6/stress_test.js`

#### `chaos_test.js` — Replication Consistency

Upload-verify-delete-verify cycle against a 3-node cluster. Verifies data consistency across replicas and tombstone propagation after delete.

| Parameter | Value |
|---|---|
| VUs | 100 (ramping) |
| Duration | ~8 min |
| Payload | 128 KB per request |

**Flow per iteration:**
1. PUT file to primary, wait 1s for replication
2. GET file from each replica, verify status 200 and body matches
3. DELETE file from primary, wait 1s for tombstone propagation
4. GET file from each replica, verify status 404

**Environment variables:** `MOMO_PRIMARY` (default `http://localhost:3333`), `MOMO_REPLICAS` (default `http://localhost:3334,http://localhost:3335`)

**Thresholds:** `http_req_failed < 0.5%`, `delete_errors < 5`, `consistency_check_rate > 99%`

**Run:** `make test-chaos` or `k6 run tests/k6/chaos_test.js`

---

### Chaos Testing (`tests/chaos/`)

Shell scripts for failure injection against a Docker Compose cluster. Require containers with `iptables` and `iproute2` installed (included in the Momo Dockerfile).

#### `network_partition.sh` — Jepsen-Style Netsplit

Simulates a network partition by injecting iptables DROP rules between one container and the others, then heals the partition and verifies cluster recovery.

**Usage:**
```bash
tests/chaos/network_partition.sh [NODE_A] [DURATION] [RESULT_DIR]
# defaults: NODE_A=momo-server0, DURATION=30, RESULT_DIR=/tmp/momo-chaos-partition
```

**Flow:**
1. Resolve container IPs via `docker inspect`
2. Inject iptables DROP rules: NODE_A ↔ NODE_B, NODE_A ↔ NODE_C (bidirectional)
3. Wait for partition duration
4. Remove all DROP rules (heal partition)
5. Wait 5s for recovery, verify `/health` endpoint on all nodes

**Spec scenario:** *"Simulating a datacenter partition during active writes"* — minority partition should reject writes while majority continues consistently.

#### `node_crash.sh` — Node Crash During Replication

Abruptly kills a node with `docker kill` (SIGKILL) during active file transfers, verifies data remains accessible on healthy replicas, then restarts the crashed node.

**Usage:**
```bash
tests/chaos/node_crash.sh [CRASH_NODE] [REMAINING_NODES] [TEST_FILE] [RESULT_DIR]
# defaults: CRASH_NODE=momo-server1, REMAINING_NODES="momo-server0 momo-server2"
```

**Flow:**
1. Generate 10 MB random test file
2. Upload to primary via `curl PUT`
3. `docker kill` the target node (kill -9)
4. Verify file retrievable and byte-identical on each remaining node via `curl GET` + `cmp`
5. Restart crashed node, measure total downtime

**Spec scenario:** *"Secondary node crash during splay replication"* — primary must catch socket error, file remains accessible on healthy replicas.

#### `slow_network.sh` — Slow Network Simulation

Injects network delay and packet loss using `tc`/`netem` to simulate degraded conditions.

**Usage:**
```bash
tests/chaos/slow_network.sh [TARGET_NODE] [DELAY_MS] [LOSS_PERCENT] [DURATION]
# defaults: TARGET_NODE=momo-server1, DELAY_MS=500, LOSS_PERCENT=5, DURATION=60
```

**Flow:**
1. Detect default network interface inside the container
2. Apply `tc qdisc add dev $IFACE root netem delay ${DELAY_MS}ms loss ${LOSS_PERCENT}%`
3. Wait for duration
4. Remove qdisc rule (`tc qdisc del`)
5. Verify cluster health

---

### Monitoring Stack (`tests/monitoring/`)

Prometheus + Grafana + node-exporter via Docker Compose. Provides real-time observability during test runs.

#### Components

| Service | Image | Port | Purpose |
|---|---|---|---|
| Prometheus | `prom/prometheus:latest` | 9090 | Metrics scraping (5s interval) |
| Grafana | `grafana/grafana:latest` | 3000 | Visualization (admin/admin) |
| node-exporter | `prom/node-exporter:latest` | 9101 | Host-level metrics |

#### Prometheus Configuration (`prometheus/prometheus.yml`)

Scrapes `/metrics` from all 3 Momo nodes (`server0:9100`, `server1:9100`, `server2:9100`) and node-exporter at `:9101`. Evaluation interval: 5s.

#### Alert Rules (`prometheus/alerts.yml`)

| Alert | Expression | For | Severity |
|---|---|---|---|
| `MomoHighErrorRate` | `rate(momo_errors_total[1m]) > 0.1` | 2m | warning |
| `MomoNodeDown` | `up{job="momo"} == 0` | 30s | critical |
| `MomoHighGoroutines` | `momo_goroutines > 1000` | 1m | warning |
| `MomoHighMemory` | `momo_memory_alloc_bytes > 1GiB` | 2m | warning |
| `MomoDiskPressure` | `momo_disk_free_bytes / (momo_disk_used_bytes + momo_disk_free_bytes) < 0.1` | 5m | warning |
| `MomoSwimAlivePeersDropped` | `momo_swim_alive_count < 2 and on(instance) momo_cluster_peers > 2` | 2m | critical |
| `MomoLeaseStarvation` | `momo_leases_active > momo_cluster_peers * 2` | 5m | warning |
| `MomoScatterTimeouts` | `rate(momo_scatter_timeout_total[5m]) > 0.5` | 5m | warning |
| `MomoReplicationFailures` | `rate(momo_replication_failures_total[5m]) > 0.1` | 5m | critical |
| `MomoBlobCountSpike` | `rate(momo_blob_count[5m]) * 60 > 1000` | 5m | warning |

#### Grafana Dashboard (`grafana/dashboards/momo-overview.json`)

21-panel dashboard auto-provisioned on startup:

| Panel | Metric | Type |
|---|---|---|
| Upload Rate | `rate(momo_uploads_total[1m])` | graph |
| Download Rate | `rate(momo_downloads_total[1m])` | graph |
| Active Connections | `sum(momo_active_connections)` | stat |
| Error Rate | `rate(momo_errors_total[1m])` | graph |
| Goroutines | `momo_goroutines` | graph |
| Memory Usage | `momo_memory_alloc_bytes / 1024 / 1024` | graph |
| Bytes Uploaded (rate) | `rate(momo_bytes_uploaded_total[1m])` | graph |
| Blob Count | `momo_blob_count` | stat |
| Stored Bytes | `momo_stored_bytes_total` | stat |
| Dedup Hits | `rate(momo_dedup_hits_total[1m])` | graph |
| Disk Free | `momo_disk_free_bytes` | stat |
| Replication Bytes | `rate(momo_replication_bytes_total[1m])` | graph |
| Replication Failures | `rate(momo_replication_failures_total[1m])` | graph |
| CAS GC Evicted Bytes | `momo_cas_gc_evicted_bytes` | stat |
| Cluster Peers | `momo_cluster_peers` | stat |
| SWIM Alive | `momo_swim_alive_count` | stat |
| SWIM Suspect | `momo_swim_suspect_count` | stat |
| SWIM Ping Latency | `momo_swim_ping_latency_seconds` | stat |
| Active Leases | `momo_leases_active` | stat |
| Scatter Queries | `rate(momo_scatter_queries_total[1m])` | graph |
| Scatter Timeouts | `rate(momo_scatter_timeout_total[1m])` | graph |

#### Prometheus `/metrics` Endpoint (`src/server/metrics_exporter.go`)

The Momo server exposes a lightweight Prometheus-format metrics endpoint when `prometheus_port` is configured in the `[metrics]` section of `momo.conf`. No external dependencies — uses `sync/atomic` counters and `runtime.ReadMemStats`. The metrics server runs in a separate goroutine on a separate port — it does not share the accept loop or connection pool with the main daemon.

**Exported metrics (all counters wired via `MetricsHook` interface):**

| Metric | Type | Description |
|---|---|---|
| `momo_connections_total` | counter | Total connections accepted |
| `momo_active_connections` | gauge | Current active connections |
| `momo_uploads_total` | counter | Total file uploads |
| `momo_downloads_total` | counter | Total file downloads |
| `momo_deletes_total` | counter | Total file deletes |
| `momo_replication_total` | counter | Total replication operations |
| `momo_errors_total` | counter | Total errors |
| `momo_bytes_uploaded_total` | counter | Total bytes uploaded (excludes dedup hits) |
| `momo_bytes_downloaded_total` | counter | Total bytes downloaded |
| `momo_uptime_seconds` | gauge | Server uptime in seconds |
| `momo_goroutines` | gauge | Current goroutine count |
| `momo_memory_alloc_bytes` | gauge | Allocated memory in bytes |
| `momo_memory_sys_bytes` | gauge | System memory in bytes |
| `momo_gc_runs_total` | counter | Total GC runs |
| `momo_build_info` | gauge | Build info (hostname label) |
| `momo_replication_bytes_total` | counter | Total bytes replicated (R5) |
| `momo_replication_failures_total` | counter | Total replication forwarding failures (R5) |
| `momo_dedup_hits_total` | counter | Total content-addressable dedup hits (R5) |
| `momo_blob_count` | gauge | Unique blobs in the CAS store, scrape-time (R5) |
| `momo_stored_bytes_total` | gauge | Total logical bytes stored, scrape-time (R5) |
| `momo_disk_used_bytes` | gauge | Disk bytes used in the data dir, scrape-time (R5) |
| `momo_disk_free_bytes` | gauge | Disk bytes free in the data dir, scrape-time (R5) |
| `momo_cas_gc_runs_total` | counter | CAS GC sweeps completed (R5) |
| `momo_cas_gc_evicted_bytes` | counter | Bytes evicted by CAS GC (R5) |
| `momo_cluster_peers` | gauge | Known P2P peers, scrape-time (R5) |
| `momo_swim_alive_count` | gauge | ALIVE peers, scrape-time (R5) |
| `momo_swim_suspect_count` | gauge | SUSPECT peers, scrape-time (R5) |
| `momo_swim_offline_count` | gauge | OFFLINE peers, scrape-time (R5) |
| `momo_swim_ping_latency_seconds` | gauge | Mean SWIM ping latency (EWMA), scrape-time (R5) |
| `momo_leases_active` | gauge | Active leases held, scrape-time (R5) |
| `momo_scatter_queries_total` | counter | Scatter-gather queries fanned out (R5) |
| `momo_scatter_timeout_total` | counter | Scatter-gather timeout expiries (R5) |
| `momo_request_latency_seconds` | histogram | Request latency by operation (opt-in via `enable_latency_histograms`, R5) |
| `momo_replication_latency_seconds` | histogram | Replication latency (opt-in, R5) |

Storage/CAS + cluster gauges are computed **at scrape time** (Statfs + bbolt read + live P2P state), never on the request hot path. Latency histograms are **opt-in**; when disabled there is zero timing overhead.

Also exposes `/health` endpoint returning `200 OK` — used by K8s liveness/readiness probes.

**Configuration:** Add `prometheus_port=9100` to `[metrics]` section. Set to `0` or omit to disable.

**Run:**
```bash
make monitoring-up   # Start Prometheus (9090) + Grafana (3000) + node-exporter (9101)
make monitoring-down  # Stop and remove containers
```

---

### Scalability Testing (`tests/kubernetes/`)

#### Kubernetes StatefulSet (`momo-statefulset.yaml`)

Deploys a 3-node Momo cluster on Kubernetes with:

| Resource | Details |
|---|---|
| StatefulSet | 3 replicas, ordered startup/shutdown |
| ConfigMap | Inline `momo.conf` with headless service DNS names |
| Headless Service | `momo-headless` for stable pod DNS (`momo-0.momo-headless`, etc.) |
| LoadBalancer Service | `momo` for external client access |
| ClusterIP Service | `momo-metrics` for Prometheus scraping |
| PVC | 10 GiB `ReadWriteOnce` per pod |
| Liveness probe | `GET /health:9100` (initial 5s, period 10s) |
| Readiness probe | `GET /health:9100` (initial 3s, period 5s) |

Ports exposed: 3333 (data), 2223 (replication), 9100 (metrics), 4450 (gossip).

P2P gossip enabled in config. Prometheus metrics endpoint on port 9100.

**Deploy:**
```bash
kubectl apply -f tests/kubernetes/momo-statefulset.yaml
```

#### K8s Scale Test (`scale-test.yaml`)

Runs k6 load generator as a K8s Deployment (5 replicas) against the Momo cluster. Each replica runs 50 VUs for 2 minutes.

**Deploy:**
```bash
kubectl apply -f tests/kubernetes/scale-test.yaml
```

#### Docker Swarm Stack (`docker-swarm.yml`)

Docker Swarm deployment with resource limits and overlay network:

| Service | CPU | Memory | Port |
|---|---|---|---|
| `momo-0` | 2.0 | 2G | 3333, 9100 |
| `momo-1` | 2.0 | 2G | 3334, 9101 |
| `momo-2` | 2.0 | 2G | 3335, 9102 |
| `k6-load` (×3) | — | — | — |

k6 workers run `load_test.js` against `momo-0:3333` and exit (no restart).

**Deploy:**
```bash
docker swarm init  # if not already in swarm mode
docker stack deploy -c tests/kubernetes/docker-swarm.yml momo
```

---

### TCP Contract Testing (`src/server/contract_test.go`)

Wire protocol byte-level assertions that prevent accidental protocol breaking changes. Run in CI via `distributed_test.yml`.

#### Contract Constants

| Constant | Value | Description |
|---|---|---|
| `contractHandshakeLen` | 84 bytes | AuthToken (64) + Timestamp (19) + Mode (1) |
| `contractMetadataLen` | 192 bytes | Hash (64) + FileName (64) + FileSize (64) |
| `contractStatusLen` | 1 byte | Metadata status code |
| `contractACKLen` | 4 bytes | Server ACK ("ACK" + 1-byte serverId, fixed-length) |

#### Tests

| Test | What it asserts |
|---|---|
| `TestContract_HandshakeLayout` | Handshake is exactly 84 bytes; `AuthTokenLength` constant is 64 |
| `TestContract_MetadataLayout` | Metadata is exactly 192 bytes |
| `TestContract_HandshakeRoundTrip` | Full TCP handshake round-trip: send 84-byte packet, receive 1-byte mode echo, verify token preservation |
| `TestContract_P2PRPCFraming` | 4-byte big-endian length prefix is correct; minimum body length is 5 bytes (type + from) |
| `TestContract_FileMetadataSizes` | `PadString` produces exactly 64-byte padded name and size fields |

**Run:** `make test-contract` or `go test -run TestContract -v -race ./src/server/...`

See also: [`docs/CONTRACT_TESTING.md`](CONTRACT_TESTING.md) for the full wire protocol specification.

---

### Distributed Testing CI Workflow (`distributed_test.yml`)

Runs on every push to `master` and every PR (path-filtered to `src/**/*.go`, `tests/k6/**`, etc.). Three jobs:

| Job | What it does | Duration |
|---|---|---|
| **TCP Contract Test** | `go test -run TestContract -v -race ./src/server/...` | ~10s |
| **k6 Load Test** | Starts 3-node `s3-tcp` cluster, installs k6 v0.54.0, runs `load_test.js` | ~1 min |
| **Chaos - Node crash** | Starts 3-node cluster, uploads 5 MB file via `curl`, kills node 1, verifies nodes 0+2 alive, restarts node 1 | ~30s |

---

## External Client Replication Test (`external_client_test.yml`)

Runs on every push to `master` and every PR (path-filtered to `src/**/*.go`, etc.). Verifies that external S3 clients (e.g., aws-cli) get correct replication mode downgrade.

**Script:** `.github/scripts/test-external-client.sh` (Makefile target: `make test-external-client`)

### Test Steps

| Step | What it verifies |
|---|---|
| **Start cluster** | 3-node `s3-tcp` cluster with `replication_order=3,2,1` and `client_side_replication_modes=3` |
| **Switch to primary-splay** | Set replication mode to 3 (primary-splay) |
| **External client PUT** | `curl` PUT without `X-Momo-Requested-Mode` (simulates aws-cli) |
| **Verify replication** | File replicated to all 3 nodes (downgraded to splay mode 2) |
| **momo CLI PUT** | momo CLI client uploads to same cluster |
| **Verify replication** | File replicated to all 3 nodes (uses primary-splay mode 3) |

---

## Metrics Test Workflow (`metrics_test.yml`)

Runs on every push to `master` and every PR that touches Go files. Verifies the Prometheus metrics exporter end-to-end.

**Script:** `.github/scripts/test-metrics.sh` (Makefile target: `make test-metrics`)

### Test Steps

| Step | What it verifies |
|---|---|
| **Start server** | 1-node cluster with `prometheus_port=9199` |
| **GET /health** | Returns HTTP 200 with body `OK` |
| **GET /metrics (before upload)** | Response has `# HELP` and `# TYPE` lines (Prometheus format) |
| **Metric presence** | All 15 expected metrics are present in the response |
| **Initial counters** | `momo_connections_total=0` before any traffic |
| **Upload file** | Client uploads a test file via `momo-tcp` protocol |
| **GET /metrics (after upload)** | `momo_uploads_total >= 1`, `momo_connections_total >= 1`, `momo_bytes_uploaded_total >= 1` |
| **Uptime** | `momo_uptime_seconds` is a positive float |
| **Build info** | `momo_build_info{hostname="..."}` label is present |

### Currently Verified Metrics

| Metric | Verified | Notes |
|---|---|---|
| `momo_connections_total` | Yes | 0 before upload, >= 1 after |
| `momo_uploads_total` | Yes | 0 before upload, >= 1 after |
| `momo_bytes_uploaded_total` | Yes | >= 1 after upload (excludes dedup hits) |
| `momo_uptime_seconds` | Yes | Positive float |
| `momo_build_info` | Yes | Has hostname label |
| `momo_active_connections` | Present | Not counter-verified |
| `momo_downloads_total` | Present | Not exercised in this test |
| `momo_deletes_total` | Present | Not exercised in this test |
| `momo_replication_total` | Present | Not exercised (1-node cluster) |
| `momo_errors_total` | Present | Checked to be 0 (clean upload) |
| `momo_bytes_downloaded_total` | Present | Not exercised in this test |
| `momo_goroutines` | Present | Runtime gauge |
| `momo_memory_alloc_bytes` | Present | Runtime gauge |
| `momo_memory_sys_bytes` | Present | Runtime gauge |
| `momo_gc_runs_total` | Present | Runtime counter |

### R5 Metrics (Phase 2-4, all implemented)

The phase 2-4 metrics from the [add-metrics-exporter spec](../openspec/changes/add-metrics-exporter/specs/observability/spec.md) are implemented in `metrics_exporter.go` and exercised by `metrics_exporter_r5_test.go` (`TestR5_*`):

| Category | Metrics | Overhead |
|---|---|---|
| **Storage** | `momo_disk_used_bytes`, `momo_disk_free_bytes` | Scrape-only (`syscall.Statfs`) |
| **Storage** | `momo_blob_count`, `momo_stored_bytes_total` | Scrape-only (bbolt read) |
| **CAS** | `momo_dedup_hits_total` | ~5ns per dedup (atomic) |
| **CAS** | `momo_cas_gc_runs_total`, `momo_cas_gc_evicted_bytes` | ~5ns per GC run (atomic) |
| **Replication** | `momo_replication_bytes_total` | ~5ns per replication (atomic) |
| **Replication** | `momo_replication_failures_total` | ~5ns per failure (atomic) |
| **Replication** | `momo_replication_latency_seconds` (histogram) | ~40ns per op (opt-in) |
| **P2P** | `momo_cluster_peers` | Scrape-only (read PeerMap) |
| **P2P** | `momo_swim_alive_count`, `momo_swim_suspect_count`, `momo_swim_offline_count` | Scrape-only (read SWIM state) |
| **P2P** | `momo_swim_ping_latency_seconds` | Scrape-only (read EWMA) |
| **Leases** | `momo_leases_active` | Scrape-only (read LeaseManager) |
| **Scatter/Gather** | `momo_scatter_queries_total`, `momo_scatter_timeout_total` | ~5ns per query (atomic) |
| **Latency** | `momo_request_latency_seconds{operation}` | ~40ns per request (opt-in, config-gated) |
| **Latency** | `momo_replication_latency_seconds` | ~40ns per op (opt-in, config-gated) |

The only phase 2-4 metric not yet implemented is `momo_lease_contentions_total`.

**Design principle:** All Phase 2-4 metrics maintain the zero-overhead guarantee — counters use `sync/atomic` (~5ns), gauges are read at scrape time only, and histograms are opt-in via `enable_latency_histograms` config flag (default: false).
