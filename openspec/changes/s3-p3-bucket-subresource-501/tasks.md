# Tasks: S3 P3 — 501 for unsupported bucket-config subresources (#912)

## 1. Phase 1: interception set + helper
- [x] Define `unsupportedBucketConfigSubresources` map (versioning/versions,
      acl, policy, cors, website, lifecycle, tagging, encryption,
      publicAccessBlock, accelerate, replication, requestPayment, logging,
      object-lock, notification)
- [x] Helper to detect a present unsupported subresource from the query values

## 2. Phase 2: dispatch interception
- [x] Intercept at the S3 dispatch root, bucket-root only (`key == ""`), all methods
- [x] Respond S3 `501` code `NotImplemented` (bounded write, store-independent)
- [x] Confirm supported subresources (`location`, `list-type`, `uploads`,
      `uploadId`/`partNumber`, `delete`, list pagination) still route unchanged

## 3. Phase 3: tests + docs
- [x] Transport tests: versioning get, tagging put, cors delete → 501 NotImplemented
- [x] Guard test: `location`, list unaffected
- [x] Object-level (`GET /bucket/key?tagging`) unchanged
- [x] `go build/vet/test ./...`, `go test -race`, `go work vendor` no-diff
- [ ] `benchstat` gate — no regression on measured hot paths
