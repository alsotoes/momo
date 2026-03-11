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

## 4. End-to-End (E2E) Integration Testing
To test the complete workflow and data transfer logic without spinning up heavy container orchestration in CI pipelines, Momo uses a dedicated bash script to simulate the network locally.

- **Location:** `.github/scripts/test-e2e.sh`
- **Workflow:**
  1. The script compiles the binary and provisions isolated `/tmp/momo-e2e/` directories for each daemon node.
  2. It launches `daemon 0`, `1`, and `2` as background processes bound to unique local ports.
  3. A temporary client connects to the system and uploads a test payload (`test_e2e_file.txt`).
  4. The script programmatically forces replication mode changes via the `change_replication` endpoints.
  5. It uses bash assertions to physically verify that the payload successfully replicated to the isolated volumes.
  6. The test script automatically tears down the background processes via bash `trap` commands on exit.

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
