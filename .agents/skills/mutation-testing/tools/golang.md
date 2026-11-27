# Go (Golang) Mutation Testing Tools (`gremlins` & `go-mutesting`)

Go has two primary automated mutation testing CLI tools, plus an AST-level / manual diff-mutation workflow for fast branch verification.

---

## 1. Gremlins (`go-gremlins/gremlins`) — Recommended for Modern Go

`gremlins` is a fast, actively maintained mutation testing tool for Go microservices and packages. It uses coverage profiles to only execute tests covering the mutated package and supports diff-only mutation.

### Installation

```bash
go install github.com/go-gremlins/gremlins/cmd/gremlins@latest
```

### Quick Start

```bash
# Run mutation testing on a specific package
gremlins unleash ./src/common

# Run with diff filter (only mutate lines changed relative to master)
gremlins unleash --diff "master" ./src/transport

# Dry-run to inspect generated mutants without executing tests
gremlins unleash --dry-run ./src/storage
```

### Configuration (`.gremlins.yaml`)

```yaml
unleash:
  test-cpu: 4
  timeout-coefficient: 3
  threshold-efficacy: 85
  threshold-mator: 80
  exclude-files:
    - ".*_mock\\.go$"
    - ".*_gen\\.go$"
mutants:
  arithmetic-base:
    enabled: true
  conditionals-boundary:
    enabled: true
  conditionals-negation:
    enabled: true
  increment-decrement:
    enabled: true
  invert-assignments:
    enabled: true
  invert-bitwise:
    enabled: true
  invert-bwassign:
    enabled: true
  invert-logical:
    enabled: true
  invert-loopctrl:
    enabled: true
  invert-negatives:
    enabled: true
  remove-self-assignments:
    enabled: true
```

### Understanding Gremlins Output

| Status | Meaning | Action |
|--------|---------|--------|
| `KILLED` | Test failed when mutation was applied | Good — test caught the bug |
| `LIVED` | All tests passed despite mutation | Bad — add/strengthen test for this line |
| `NOT COVERED` | No test executed the mutated line | Add unit test covering this branch |
| `TIMED OUT` | Mutation caused infinite loop / deadlock | Counted as killed (verify bounded loops per Rule 24) |
| `NOT VIABLE` | Mutation failed to compile | Ignored |

---

## 2. `go-mutesting` (AST Expression & Statement Mutator)

`go-mutesting` includes statement-removal and branch-removal mutators (`branch/if`, `branch/else`, `statement/remove`) in addition to arithmetic and comparison mutators.

### Installation

```bash
go install github.com/avito-tech/go-mutesting/cmd/go-mutesting@latest
```

### Running on Changed Packages

```bash
# Mutate a single file or package
go-mutesting ./src/common/crush.go

# Run with race detector off for speed during mutation sweep, then verify survivors with -race
go-mutesting --exec-timeout=10 ./src/storage/...
```

---

## 3. Zero-Install Agentic Mutation Workflow (Fastest for PR / Branch Review)

When `gremlins` or `go-mutesting` is not installed in the sandbox, perform **Targeted Surgical Mutation Verification** directly using `go test`:

1. **Identify changed Go files on the branch:**
   ```bash
   rtk git diff master...HEAD --name-only | grep '\.go$' | grep -v '_test\.go$'
   ```
2. **List high-risk mutation points in the diff:**
   - Boundary comparisons (`<`, `<=`, `>`, `>=`, `== MaxSize`)
   - POSIX error returns (`syscall.EACCES`, `syscall.EBADMSG`, `syscall.ETIMEDOUT`, `syscall.EIO`)
   - Panic recovery (`defer common.RecoverErr*`)
   - Bitwise / zero-alloc slice bounds (`buf[:n]`, `counter&127 == 0`)
3. **Verify each mutant is killed:**
   - Temporarily apply a 1-line mutation (or trace the existing `_test.go` assertions).
   - Run `go test -run <TestName> ./src/<pkg>`.
   - Confirm the test **FAILS**. If it passes, the mutant survived — write or strengthen the table-driven test case (e.g., boundary value, `errors.Is(err, syscall.E...)`, non-identity input) and revert the mutation.
   - Confirm `rtk git diff` on production code is clean after restoring.

---

## 4. CI/CD Integration (GitHub Actions Diff-Only Gate)

Running full-repository mutation testing on every PR is slow; always use `gremlins unleash --diff` so only changed lines in the PR are mutated:

```yaml
name: Go Mutation Testing

on:
  pull_request:
    branches: [master]
    paths:
      - "src/**/*.go"

jobs:
  gremlins:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0 # Required for --diff against origin/master

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Install Gremlins
        run: go install github.com/go-gremlins/gremlins/cmd/gremlins@latest

      - name: Run Diff-Scoped Mutation Testing
        run: gremlins unleash --diff "origin/master" --threshold-efficacy=80 ./src/...
```

---

## 5. Performance & Troubleshooting Tips for Go

1. **Scope by Package (`./src/<pkg>`)**: Go compiles each package into a separate test binary. Mutating one package at a time avoids recompiling the whole workspace.
2. **Disable `-race` During Broad Gremlins Sweeps**: Race instrumentation increases compile time and execution time by 2–5×. Let `gremlins` run normal `go test` first to find surviving logic/boundary mutants, then run `go test -race` on your new tests.
3. **Avoid Mutating Generated & Vendor Code**: Always exclude `vendor/`, `*_gen.go`, `*_mock.go`, and `*.pb.go` in `.gremlins.yaml`.
4. **Slow Integration Tests**: Tag Docker/network integration tests with `//go:build integration` so `gremlins` only executes fast (<10ms) unit tests per mutant.

