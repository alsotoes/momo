# Momo Testing Guide

Momo is built with reliability and concurrency in mind. Testing a distributed system involves verifying individual components, managing concurrent routines, and ensuring that network topologies and data replication correctly handle edge cases.

This document outlines the testing strategies implemented in Momo and how to run them.

## 1. Unit Tests and Core Functionality
We rely on Go's built-in `testing` package to verify individual functions and methods in isolation.

- **Location:** Unit tests are located adjacent to the files they test (e.g., `src/server/server_test.go`, `src/common/config_test.go`).
- **Table-Driven Tests:** Many of our tests (like configuration parsing and metrics evaluations) use table-driven test patterns to ensure we evaluate numerous input scenarios clearly and concisely.
- **Mocking:** For networking and system metrics, we employ custom interfaces and mock servers (using `net.Pipe` or dummy TCP listeners) to avoid external dependencies during unit tests.

**To run unit tests:**
```bash
make test
```

## 2. Concurrency and Leak Prevention
Distributed systems are heavily concurrent. A single hanging goroutine can eventually exhaust system resources (a goroutine leak). 

To prevent this, Momo integrates **Uber's `goleak`** into its test suite. 

- **Implementation:** Tests spanning concurrent boundaries (like network dials, daemon listeners, and replication fan-outs) invoke `defer goleak.VerifyNone(t)`. 
- **Context Handling:** Network operations respect `context.Context`. Graceful shutdowns are tested by cancelling contexts, ensuring the daemon immediately halts acceptance loops and tears down open connections without leaking routines.

**Race Conditions:** All Go tests are run with the `-race` flag enabled by default via the `Makefile` to detect unsafe parallel memory access.

## 3. Load and Stress Testing (Benchmarking)
To ensure the primary daemon can handle bursts of file uploads under heavy load, we utilize Go's benchmark tooling.

- **Location:** `src/server/server_benchmark_test.go`
- **What it does:** The `BenchmarkConcurrentUploads` function spawns a daemon and blasts it with `b.N` concurrent client file uploads. It measures memory allocations and operation speeds under stress.

**To run benchmarks:**
```bash
make benchmark
```

## 4. End-to-End (E2E) Docker Integration
Unit testing mock connections is not a substitute for a real network topology. Momo uses Docker Compose to execute full End-to-End integration tests.

- **Location:** `.github/scripts/test-e2e.sh`
- **Workflow:**
  1. The script spins up the complete `docker-compose.yml` environment containing 3 server nodes on a dedicated Docker network.
  2. It polls the health checks of the primary daemon to ensure the cluster is ready.
  3. A temporary client container is created and executes the upload of a test payload (`test_e2e_file.txt`).
  4. The script uses `docker exec` to physically verify that the payload successfully replicated to the isolated `/root/received_files/` volumes across `server0`, `server1`, and `server2`.
  5. The cluster is torn down.

**To run E2E tests locally:**
```bash
make test-e2e
```

## 5. Test Coverage
Test coverage reports evaluate the percentage of statements exercised by the test suite. 

**To view the coverage report:**
```bash
make coverage
```
This runs the tests, exports `coverage.out`, and automatically opens the visual HTML coverage report via `go tool cover`.
