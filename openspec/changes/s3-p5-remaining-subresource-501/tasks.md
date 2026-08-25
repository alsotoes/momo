# Tasks: S3 P5 — 501 for remaining unsupported S3 operations (#920)

## 1. Phase 1: reject-set extensions
- [x] Add `analytics`, `inventory`, `metrics`, `intelligent-tiering` to
      `unsupportedBucketConfigSubresources`
- [x] Add `select` to `unsupportedObjectSubresources`
- [x] Confirm no collision with supported subresources / metrics endpoints

## 2. Phase 2: UploadPartCopy interception
- [x] Intercept `PUT` with `?uploadId`+`?partNumber` AND `X-Amz-Copy-Source`
      before the UploadPart handler → `501 NotImplemented`
- [x] Confirm plain UploadPart without `X-Amz-Copy-Source` unchanged

## 3. Phase 3: tests + docs
- [x] Transport tests: analytics get, inventory put, metrics get,
      intelligent-tiering get, select post → 501 NotImplemented
- [x] Guard test: plain UploadPart (no copy-source) still 404/200
- [x] `go build/vet/test ./...`, `go test -race`, `go work vendor` no-diff
- [ ] `benchstat` gate — no regression on measured hot paths (CI)
