# projectmem - momo

_Last updated: 2026-11-24_

## Project purpose
Replace this placeholder with a concise description of what this project does, who it serves, and the main technologies or runtime assumptions.

## Recent issues
- [DONE] #legacy_ec6d Legacy issue: fix(security): add nosecret so scanners wont jump (fixed)
- [DONE] #legacy_df60 Legacy issue: Merge pull request #1374 from alsotoes/fix/1373-part-digest-check (fixed)
- [DONE] #legacy_c773 Legacy issue: fix(s3): verify part digests before recovering an upload (#1373) (fixed)
- [DONE] #legacy_99d0 Legacy issue: Merge pull request #1368 from alsotoes/fix/1367-ephemeral-test-ports (fixed)
- [DONE] #legacy_2c7f Legacy issue: fix(ci): bind ephemeral ports in transport tests (#1367) (fixed)

## Decisions
- Retire MemPalace and remove Rule 89, mempalace.yaml, and harness manifest references in favor of projectmem and codegraph [openspec/config.yaml:178]

## Notes
- test(s3): a failed part write must not register a part (#1365)
- Merge pull request #1366 from alsotoes/feat/1365-upload-part-copy
- feat(metrics): expose multipart upload count and spill footprint (#1369)
- Merge pull request #1370 from alsotoes/feat/1369-multipart-metrics
- feat(s3): recover in-flight multipart uploads after a restart (#1371)
- test(s3): cover manifest write and validation failures (#1371)
- Merge pull request #1372 from alsotoes/feat/1371-multipart-recovery
- feat(s3): implement object tagging (#1375)
- refactor(s3): hoist the tagging route messages (#1375)
- Merge pull request #1376 from alsotoes/feat/1375-object-tagging

## Key files
- `.github/data/benchmark_history.csv`
- `docs/GUIDES/TESTING.md`
- `src/transport/s3_communicator.go`
- `docs/ARCHITECTURE.md`
- `docs/REFERENCE/COMPATIBILITY.md`
- `src/transport/s3_uploadpartcopy_test.go`
- `src/transport/s3_copy_helpers_test.go`
- `src/transport/s3_multipart_manifest_test.go`
- `src/transport/s3_communicator_test.go`
- `src/transport/s3_multipart_reaper.go`
- `openspec/changes/s3-multipart-restart-recovery/tasks.md`
- `src/transport/s3_multipart_manifest.go`
- `openspec/changes/s3-multipart-restart-recovery/specs/s3-multipart-recovery/spec.md`
- `src/transport/s3_tagging.go`
- `openspec/changes/s3-upload-part-copy/tasks.md`
- `openspec/changes/s3-upload-part-copy/specs/s3-upload-part-copy/spec.md`
- `src/server/server.go`
- `docs/adr/0111-s3-upload-part-copy.md`
- `src/server/background_maintenance_test.go`
- `docs/adr/0111-s3-upload-part-copy.no-blog.md`

## Open questions
- None logged yet.
