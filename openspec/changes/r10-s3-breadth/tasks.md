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
- [ ] `src/s3/lifecycle.go`: New file — `LifecycleRule` struct, `LifecycleEngine`
- [ ] `LifecycleEngine`: `Evaluate(bucket)` — iterates objects, matches prefix/filter, executes transitions/expirations
- [ ] `src/transport/s3_communicator.go`: `handlePutLifecycle` / `handleGetLifecycle` / `handleDeleteLifecycle`
- [ ] `src/server/server.go`: Register lifecycle background worker (interval from `[s3] lifecycle_worker_interval`)
- [ ] Transitions: copy object to new storage class (update `StorageClass` in metadata)
- [ ] Expirations: delete object (create delete marker if versioned; permanent delete if not)

### Notification
- [ ] `src/s3/notification.go`: New file — `NotificationConfig`, `EventType`, `NotificationWorker`
- [ ] `src/transport/s3_communicator.go`: `handlePutNotification` / `handleGetNotification` / `handleDeleteNotification`
- [ ] `NotificationWorker`: async HTTP POST with exponential backoff; retry up to `[s3] notification_retry_max`
- [ ] Event filtering: prefix/suffix filter on object key

### Object Lock
- [ ] `src/storage/storage.go`: Add `ObjectLockConfig` to `ObjectMeta` (`Mode`, `RetainUntilDate`, `LegalHold`)
- [ ] `src/transport/s3_communicator.go`: `handlePutObjectLock` (bucket-level, only at creation)
- [ ] `src/transport/s3_communicator.go`: In `handlePutObject` / `handleDeleteObject` / `handleCopyObject` — enforce retention/legal hold
- [ ] `src/transport/s3_communicator.go`: `handleGetObject` / `handleHeadObject` — return `x-amz-object-lock-*` headers

### Config
- [ ] `src/common/config.go`: Add `[s3]` keys: `lifecycle_worker_interval`, `notification_worker_concurrency`, `notification_retry_max`
- [ ] `conf/momo.conf`: Document new S3 breadth keys

## Testing

- [x] `TestS3Versioning_FullStackFlow` — enable versioning, record versions, GET ?versions, GET specific version, DELETE with/without versionId (marker + revive + permanent)
- [x] `TestS3Versioning_UnversionedDeleteUnchanged` — unversioned buckets keep plain delete semantics
- [x] Handler/error-branch coverage: PUT malformed/invalid status, GET/HEAD versionId missing (404 NoSuchVersion), 501 without versioning capability, max-keys clamp, prefix filter
- [ ] `TestLifecycleTransitions` — rule with transition after N days; verify storage class change
- [ ] `TestLifecycleExpiration` — rule with expiration; verify object deleted
- [ ] `TestNotification` — PUT notification config; trigger event; verify webhook received
- [ ] `TestObjectLock` — enable lock; PUT with retention; verify overwrite/delete denied; legal hold
- [ ] `go test -race ./...` — all tests pass
- [ ] `make test` — full suite passes

## Compliance

- [ ] OpenSpec change authored (Rule 73)
- [ ] ADR generated via `make adr-sync` (Rule 77/78)
- [ ] Blog post shipped (Rule 76)
- [ ] PR body includes `Resolves #938` (Rule 11)