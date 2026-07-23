# Testing & CI Pipeline

This document describes every test suite and validation step that runs in the Momo CI/CD pipeline.

## Pipeline Overview

| Workflow | File | Trigger | Purpose |
|---|---|---|---|
| Go | `go.yml` | push to master, PRs | Build, unit tests, benchmarks, E2E, coverage |
| Smoke Test | `smoke_test.yml` | push to master, PRs | Multi-protocol file replication verification |
| Scale & CAS E2E | `scale_cas_test.yml` | push to master, PRs | CAS storage scale testing |
| P2P Gossip E2E | `p2p_test.yml` | push to master, PRs | P2P gossip convergence + failure detection |
| Performance Comparison | `benchmark_compare.yml` | PRs, push to master | Benchmark regression detection (>5% threshold) |
| Go Version Consistency | `verify_go_version.yml` | PRs, push to master | Go version sync across all config files |
| Gemini AI Reviewer | `gemini_reviewer.yml` | PRs | AI code review (security, performance, architecture) |
| Auto Reviewer | `auto_reviewer.yml` | PR opened/reopened | Initial automated review |
| Weekly Sanity | `weekly_sanity.yml` | Weekly cron (Sun 00:00 UTC) | Full suite + security audit |

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

### Modules under test

```
./src/common ./src/transport ./src/client ./src/metrics ./src/p2p ./src/server ./src/storage
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

# Coverage report
make coverage

# Format check
make fmt

# Vendor sync
make vendor
```
