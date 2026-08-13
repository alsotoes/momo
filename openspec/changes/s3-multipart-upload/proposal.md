# Change: S3 Multipart Upload Support
**Related Issues:**
- https://github.com/alsotoes/momo/issues/764

## Why
AWS S3 clients (aws-cli, aws-sdk-go-v2, boto3) automatically switch to multipart upload for objects above a size threshold (8 MB default). momo's S3 gateway implemented only monolithic `PUT /bucket/key`. Multipart operations were entirely unimplemented: any object uploaded over the SDK threshold failed. This broke large-file support — a core AWS S3 use case.

## What Changes
- **All 6 multipart endpoints are now intercepted** in `HandshakeServer` and handled end-to-end (bypass momo framing, return `ErrRequestHandled`):
  - `POST /bucket/key?uploads` → `CreateMultipartUpload`: returns an upload ID.
  - `PUT /bucket/key?uploadId=X&partNumber=N` → `UploadPart`: stores the part body in memory keyed by upload ID.
  - `POST /bucket/key?uploadId=X` → `CompleteMultipartUpload`: assembles all parts in order, computes SHA-256, calls `store.Put()` with the assembled content, returns `CompleteMultipartUploadResult` XML.
  - `DELETE /bucket/key?uploadId=X` → `AbortMultipartUpload`: cleans up tracked parts (204 No Content).
  - `GET /bucket/key?uploadId=X` → `ListParts`: returns S3 ListPartsResult XML with per-part metadata.
  - `GET /?uploads` (or `GET /bucket?uploads`) → `ListMultipartUploads`: returns S3 ListMultipartUploadsResult XML with active uploads.
- **In-memory tracking** via `map[string]*multipartUpload` protected by `sync.Mutex`. Each upload holds a list of `multipartPart{partNumber, etag, data}`.
- **CompleteMultipartUpload** assembles parts sorted by part number, hashes with SHA-256 (becomes ETag), and stores the assembled object through `store.Put()` so the object flows through the normal momo CAS/persistence path.
- **Tests**: `TestS3MultipartUpload_FullFlow` (Create → 2× UploadPart → ListParts → Complete) and `TestS3MultipartUpload_Abort` (Create → Abort → ListParts returns 404).

## Non-Goals
- No disk spooling for large parts (current in-memory approach is fine for typical test/CI payloads; production use could spool to disk).
- No S3-compliant ETag computation (AWS computes `MD5(partETags)`, momo uses SHA-256 as the content hash — acceptable for S3-compatible gateways).
- No multipart upload copy (UploadPartCopy) — standard S3 but rarely used.
- No changes to the momo wire protocol between peers.

