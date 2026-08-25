# Tasks: S3 P4 — 501 for unsupported object-level subresources (#914)

## 1. Phase 1: interception set + helper
- [x] Define `unsupportedObjectSubresources` map (tagging, acl, versionId,
      retention, legal-hold)
- [x] Helper to detect a present unsupported object subresource from the query values

## 2. Phase 2: dispatch interception
- [x] Intercept at the S3 dispatch root, object-key only (`key != ""`), all methods
- [x] Respond S3 `501` code `NotImplemented` (bounded write, store-independent)
- [x] Confirm supported object params (`uploadId`, `partNumber`) still route unchanged
- [x] Confirm bucket-root bucket-config rejection (#912/#913) retained

## 3. Phase 3: tests + docs
- [x] Transport tests: tagging get, acl put, versioned delete → 501 NotImplemented
- [x] Guard test: multipart (`uploadId`/`partNumber`) unaffected
- [x] Guard test: bucket-root `?versioning` still 501
- [x] `go build/vet/test ./...`, `go test -race`, `go work vendor` no-diff
- [ ] `benchstat` gate — no regression on measured hot paths (CI)
