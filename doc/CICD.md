# Momo CI/CD Workflows

Momo uses GitHub Actions to automate testing, static analysis, and code review processes upon every commit and Pull Request.

Our Continuous Integration (CI) and Continuous Deployment (CD) strategy focuses on executing fast unit checks followed by heavy end-to-end verifications in production-like environments.

## Workflows

All workflows are located in the `.github/workflows/` directory.

### 1. Main Pipeline (`go.yml`)
This is the core CI pipeline triggered on every push and PR to the `master` branch.

- **Build:** Compiles the Momo binary (`make build`) to catch compilation errors early.
- **Test:** Runs all Unit Tests, integration logic, Race Detectors, and Goroutine leak verifications (`make test`).
- **Benchmark:** Executes the stress test suite against the Daemon to detect severe performance regressions (`make benchmark`).
- **E2E Integration:** Provisions a full Docker Compose cluster to ensure network data consistency using the `.github/scripts/test-e2e.sh` script (`make test-e2e`).
- **Coverage:** Runs the coverage suite and automatically uploads the `coverage.out` file as a GitHub artifact so developers can review the exact coverage metrics of the PR without checking out the code locally.

### 2. Smoke Tests (`smoke_test.yml`)
While the E2E tests focus on baseline data transfer across Docker nodes, the `smoke_test.yml` workflow focuses heavily on the polymorphic metrics controller.

- Validates each replication mode (Chain, Splay, PrimarySplay) individually.
- Spins up daemons on the host OS and dynamically constructs timestamp payloads simulating replication shifts.
- Uses `exit 1` loops and asserts that physical data files exist strictly on the correct subset of server nodes depending on the replication layout.

### 3. Auto Reviewer (`auto_reviewer.yml`)
To enforce code review best practices without manual intervention, this workflow listens for Pull Requests (opened, reopened, ready_for_review).

- Automatically uses the GitHub REST API to request a review from the designated lead maintainer (`alsotoes`).
- Intelligently skips assignment if the lead maintainer is the author of the PR.