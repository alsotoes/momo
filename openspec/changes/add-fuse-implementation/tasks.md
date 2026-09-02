# FUSE Implementation Tasks

## Phase 1: Audit and Planning (1-2 days)
- [x] 1.1 Review current `src/momofs/fuse.go` — document all 22 callback signatures and their usage sites
- [x] 1.2 Audit `consistency=cached` usage across config and code — identify all 8+ references
- [x] 1.3 Inventory platform targets: Linux (bare-metal/Docker), macOS (Docker Desktop), Windows (if any)
- [x] 1.4 Decide on go/no-go for go-fuse/v2 migration vs VirtioFS-only

## Phase 2: go-fuse/v2 Adapter (3-5 days)
- [x] 2.1 Fork/adapt `hanwen/go-fuse/v2` to provide the 22 FUSE callback adaptations
- [x] 2.2 Map each prior callback to go-fuse/v2 equivalent (see spec.md mapping table)
- [ ] 2.3 Implement splice/ReadResultPipe for zero-copy read path where kernel supports it
- [x] 2.4 Replace `src/momofs/fuse.go` with adapter code; ensure `go build ./...` succeeds
- [x] 2.5 Write unit tests for each adapted callback (happy path + error path)

## Phase 3: consistency=cached Deprecation (1-2 days)
- [x] 3.1 Add deprecation AUDIT log in config reload path when `consistency = "cached"` is set
- [x] 3.2 Remove `consistency` from the public config schema (keep internal only with warning)
- [ ] 3.3 Update `docs/TESTING.md` with deprecation test results
- [x] 3.4 Verify no behavioral change when flag is set/unset

## Phase 4: Cross-Platform Fallback (2-3 days)
- [ ] 4.1 Implement fallback path for platforms without VirtioFS/go-fuse/v2
- [ ] 4.2 Add WARNING log entry guiding users to Docker Desktop on macOS for best experience
- [ ] 4.2 Add WARNING log entry guiding Linux users toward `go-fuse/v2` with splice

## Phase 5: Validation and Documentation (2-3 days)
- [x] 5.1 Build momofs on Linux with go-fuse/v2 — confirm 0 compile errors
- [ ] 5.2 Run k6 load test (10→20 VUs, 5 min) — document throughput vs prior custom protocol
- [ ] 5.3 Measure `/metrics` handler latency under load — document p99
- [ ] 5.4 Measure memory overhead delta vs prior implementation
- [x] 5.5 Run integration test: mount, 1000 upload/download cycles, data integrity verify
- [ ] 5.6 On macOS: verify Docker VirtioFS is in use (no momofs FUSE process); `consistency=cached` logged and ignored
- [ ] 5.7 Update `docs/TESTING.md` and `docs/COMPATIBILITY.md` with new FUSE matrix

## Definition of Done

- [ ] `src/momofs/fuse.go` replaced with go-fuse/v2 adapter
- [ ] `consistency=cached` deprecated with AUDIT log
- [ ] Momofs builds and runs on Linux with go-fuse/v2
- [ ] Momofs on macOS uses Docker VirtioFS (no code change needed)
- [ ] No FUSE daemon processes in `ps aux`
- [ ] All unit tests pass (`go test ./...`)
- [ ] Integration tests pass (mount + data cycles)
- [ ] Documentation updated (`TESTING.md`, `COMPATIBILITY.md`)
- [ ] OpenSpec change `add-fuse-implementation` committed and pushed