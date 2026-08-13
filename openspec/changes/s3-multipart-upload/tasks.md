# Tasks: S3 Multipart Upload Support (issue #764)

## 1. Phase 1: Multipart state tracking + handler functions
- [x] 1.1 Add `multipartUpload` / `multipartPart` types and global `uploads` map with `sync.Mutex` protection.
- [x] 1.2 `generateUploadID()` — random hex upload ID.
- [x] 1.3 `handleCreateMultipartUpload` — stores empty upload state, returns UploadId XML.
- [x] 1.4 `handleUploadPart` — reads body, computes SHA-256/etag, stores part.
- [x] 1.5 `handleCompleteMultipartUpload` — sorts parts, assembles, hashes, calls `store.Put()`.
- [x] 1.6 `handleAbortMultipartUpload` — deletes upload state, returns 204.
- [x] 1.7 `handleListParts` — returns S3 ListPartsResult XML.
- [x] 1.8 `handleListMultipartUploads` — returns S3 ListMultipartUploadsResult XML.
- [x] 1.9 Helper `writeXMLResponse` for common 200 XML response pattern.

## 2. Phase 2: Routing intercepts in HandshakeServer
- [x] 2.1 POST: `?uploads` → CreateMultipartUpload, `?uploadId` → CompleteMultipartUpload.
- [x] 2.2 PUT: `?uploadId+partNumber` → UploadPart (before CopyObject).
- [x] 2.3 DELETE: `?uploadId` → AbortMultipartUpload.
- [x] 2.4 GET: `?uploadId` → ListParts, `?uploads` → ListMultipartUploads.

## 3. Phase 3: Tests
- [x] 3.1 `TestS3MultipartUpload_FullFlow` — Create → 2× UploadPart → ListParts → Complete.
- [x] 3.2 `TestS3MultipartUpload_Abort` — Create → Abort → ListParts returns 404.
- [x] 3.3 All transport tests pass under `-race`.

## 4. Documentation
- [ ] 4.1 Create `openspec/changes/s3-multipart-upload/` (proposal, tasks).
- [ ] 4.2 Update `docs/PROTOCOL.md` — document multipart upload interception strategy.

## 5. Validation
- [ ] 5.1 `gofmt`, `go vet`, full per-module test suites.
- [ ] 5.2 `go work vendor` produces no diff (Rule 25).
- [ ] 5.3 Commit, Rule 58 branch check, push.
- [ ] 5.4 Open PR with `Resolves #764`, wait for checks/approval, merge, close issue, Rule 71 gate.

