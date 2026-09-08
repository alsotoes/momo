# Spec: Go 1.26 Upgrade

## Requirements

### GO-1: Root module and workspace declare Go 1.26

**Requirement:** The root `go.mod` and `go.work` MUST declare Go 1.26 as the required version.

**Scenario: Build and test run on Go 1.26**

Given the project is cloned,
When `go build ./...` and `go test ./...` run,
Then they succeed on Go 1.26.x without version errors.

### GO-2: All CI workflows specify Go 1.26

**Requirement:** Every GitHub Actions workflow specifying `go-version` MUST use `'1.26.x'`.

**Scenario: CI runs on Go 1.26**

Given a PR is opened,
When CI runs,
Then all jobs use Go 1.26.x and no job uses 1.25.x.

### GO-3: verify_go_version.yml validates 1.26

**Requirement:** The version consistency checker MUST accept Go 1.26.x and reject 1.25.x.

**Scenario: Version check passes on 1.26**

Given `go.work` declares `go 1.26.x`,
When `verify_go_version.yml` runs,
Then it reports success and no mismatches.

### GO-4: All module go.mod files synced to 1.26

**Requirement:** After `go work sync`, all module `go.mod` files declare Go 1.26.

**Scenario: Modules consistent**

Given `go work sync` completes,
Then every `go.mod` under `src/*/` declares `go 1.26.x`.

### GO-5: Full test suite passes on Go 1.26

**Requirement:** The complete test suite MUST pass on Go 1.26 with race detector.

**Scenario: Full suite green**

Given the codebase on Go 1.26,
When `make test` runs,
Then all 9 workspace modules pass with `-race`.

## Non-Goals

- No quic-go version upgrade (separate PR after 1.26 lands)
- No Go 1.26-specific language features used in source code
- No changes to pinned x/perf benchmark version