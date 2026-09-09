# Tasks: R10 — S3 Lifecycle, Versioning, Notification, Object Lock Breadth

## Implementation

### Versioning
- [ ] `src/storage/storage.go`: Add `VersionedBlob` struct with `VersionID`, `IsDeleteMarker`, `IsLatest`
- [ ] `src/storage/storage.go`: Add `PutBlobVersioned(bucket, key, data, versionID) (newVersionID string)`
- [ ] `src/storage/storage.go`: Add `GetBlobVersioned(bucket, key, versionID) (data, metadata)`
- [ ] `src/storage/storage.go`: Add `ListVersions(bucket, prefix, delimiter, keyMarker, versionIDMarker, maxKeys)`
- [ ] `src/transport/s3_communicator.go`: `handlePutVersioning` — parse `<VersioningConfiguration>`; store bucket versioning status
- [ ] `src/transport/s3_communicator.go`: `handleGetVersions` — implement `GET ?versions` with pagination
- [ ] `src/transport/s3_communicator.go`: `handleGetObject` — support `versionId` query param
- [ ] `src/transport/s3_communicator.go`: `handleDeleteObject` — create delete marker if no `versionId`; permanent delete if `versionId` provided
- [ ] `src/storage/metadata.go`: Extend `ObjectMeta` with `VersionID`, `IsDeleteMarker`, `IsLatest`, `VersionSequence`

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

- [ ] `TestVersioning` — enable versioning, PUT multiple versions, GET specific version, DELETE with/without versionId
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