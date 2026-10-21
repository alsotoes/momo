# Tasks: R10 — S3 Lifecycle, Versioning, Notification, Object Lock Breadth

## Implementation

### Versioning (Phase 1 — SHIPPED)
- [x] `src/storage/versioning.go`: `VersionRecord` (VersionID/Hash/Size/Timestamp/IsLatest/IsDeleteMarker + wire-less Key) — CAS dedup preserved: N versions of identical content cost one physical blob
- [x] `src/storage/versioning.go`: `SetBucketVersioning`/`BucketVersioning`/`IsVersioned` (bbolt `versioning` bucket)
- [x] `src/storage/versioning.go`: `RecordVersion`/`RecordDeleteMarker` (opaque UUIDv4 version ids — S3 mutation identity)
- [x] `src/storage/versioning.go`: `LatestVersion`/`GetVersion`/`GetVersionContent`/`DeleteVersion` (latest re-pointing; marker removal revives the previous version)
- [x] `src/storage/versioning.go`: `ListVersions` (S3 order: key asc, last-modified desc; key-marker/version-id-marker pagination)
- [x] `src/storage/versioning.go`: `RepointNamespace` (flat GETs follow the latest data version / 404 under a marker)
- [x] `src/transport/s3_versioning.go`: `handlePutVersioning`/`handleGetVersioning`/`handleGetVersions` (ListVersionsResult XML with `Version`+`DeleteMarker` elements)
- [x] `src/transport/s3_communicator.go`: GET/HEAD `?versionId` routes (+ 501 when the store lacks the capability)
- [x] `src/transport/s3_communicator.go`: versioned DELETE — marker without `versionId` (204 + `x-amz-delete-marker: true`), permanent with `versionId` (204 + revived namespace)
- [x] `src/server/server.go`: `recordVersionIfVersioned` ingest hook — every S3-external PUT path records a version when the bucket is versioned (native-protocol writes never do)
- [x] `src/transport/s3_communicator.go`: multipart completion records a version + echoes `<VersionId>` in the response XML
- [x] 501 maps updated: `versioning`/`versions`/`versionId` removed from the reject sets (now real implementations)
- [x] Tests: `src/storage/versioning_test.go` (9), `src/transport/s3_versioning_test.go` (14, incl. full-stack flow through the real CASStore)

### Versioning follow-ups (tracked)
- [ ] Cross-node version-record replication via the R6 metadata RPC (Phase 1 keeps version records node-local; PR documents this ceiling)
- [ ] `x-amz-version-id` response header on plain PUTs (response is written before the body streams; multipart already echoes it)

### Lifecycle
- [x] `src/storage/lifecycle.go`: New file — `LifecycleRule`/`LifecycleConfig` + `EvaluateLifecycle` engine (implemented in the storage layer per the protocol/cluster separation, ADR 0053; `src/s3/` does not exist)
- [x] `EvaluateLifecycle(bucket, now)`: iterates objects, matches prefix, applies expiration/transition
- [x] `src/transport/s3_lifecycle.go`: `handlePutLifecycle` / `handleGetLifecycle` / `handleDeleteLifecycle` (+ `LifecycleStore` capability, 501 without it)
- [x] `src/server/server.go`: `startLifecycleWorker` (interval from `[s3] lifecycle_worker_interval`)
- [x] Transitions: update `x-amz-storage-class` in the object's S3 metadata
- [x] Expirations: delete object (delete marker if versioned; permanent delete if not)

### Notification
- [x] `src/storage/notification.go`: New file — `NotificationConfig`, `EventType`, `NotificationConfig` CRUD, `DispatchNotification`
- [x] `src/transport/s3_notification.go`: New file — `NotificationStore` interface, `ParseNotificationConfiguration`, `FormatNotificationConfiguration`, handlers
- [x] `src/transport/s3_communicator.go`: `handlePutNotification` / `handleGetNotification` / `handleDeleteNotification` + `NotificationStore` capability
- [x] `src/server/server.go`: `NotificationStore` capability wiring; `afterS3ObjectWriteBucket` dispatches ObjectCreated on every S3 write; `SetNotificationRetryMax` propagates the retry budget to the gateway
- [x] Webhook delivery: sync HTTP POST with exponential backoff; retries network errors and 5xx up to `[s3] notification_retry_max`, returns immediately on 4xx
- [x] Event filtering: prefix/suffix filter on object key
- [x] Delete notifications: ObjectRemoved fired from the single-DELETE and batch-DELETE paths
- [x] Multipart: ObjectCreated:CompleteMultipartUpload fired on assembled upload
- [x] Tests: `src/storage/notification_test.go` (delivery, filtering, retry backoff, exhausted retries, 4xx no-retry, unsupported target), `src/transport/s3_notification_test.go` (parsing, formatting, handler + error paths, dispatch helper), `src/server/s3_notification_hook_test.go` (ingest hook end-to-end)

### Object Lock
- [ ] `src/storage/storage.go`: Add `ObjectLockConfig` to `ObjectMeta` (`Mode`, `RetainUntilDate`, `LegalHold`)
- [ ] `src/transport/s3_communicator.go`: `handlePutObjectLock` (bucket-level, only at creation)
- [ ] `src/transport/s3_communicator.go`: In `handlePutObject` / `handleDeleteObject` / `handleCopyObject` — enforce retention/legal hold
- [ ] `src/transport/s3_communicator.go`: `handleGetObject` / `handleHeadObject` — return `x-amz-object-lock-*` headers

### Config
- [x] `src/common/config.go`: `[s3] lifecycle_worker_interval` and `notification_retry_max` (default 3, validated ≥ 1)
- [x] `conf/momo.conf`: Documented the `[s3]` section

## Testing

- [x] `TestS3Versioning_FullStackFlow` — enable versioning, record versions, GET ?versions, GET specific version, DELETE with/without versionId (marker + revive + permanent)
- [x] `TestS3Versioning_UnversionedDeleteUnchanged` — unversioned buckets keep plain delete semantics
- [x] Handler/error-branch coverage: PUT malformed/invalid status, GET/HEAD versionId missing (404 NoSuchVersion), 501 without versioning capability, max-keys clamp, prefix filter
- [x] `TestEvaluateLifecycleTransition` (storage) — verifies the storage-class change
- [x] `TestEvaluateLifecycleExpiration` (storage) — verifies deletion
- [x] `TestNotification` — PUT notification config; trigger event; verify webhook received
- [ ] `TestObjectLock` — enable lock; PUT with retention; verify overwrite/delete denied; legal hold
- [x] `go test ./src/storage/ ./src/transport/ ./src/server/ ./src/common/` — all pass
- [x] `make test` — full suite passes

## Compliance

- [ ] OpenSpec change authored (Rule 73)
- [ ] ADR generated via `make adr-sync` (Rule 77/78)
- [ ] Blog post shipped (Rule 76)
- [ ] PR body includes `Resolves #938` (Rule 11)