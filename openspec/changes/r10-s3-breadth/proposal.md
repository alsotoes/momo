# Change: R10 — S3 Lifecycle, Versioning, Notification, Object Lock Breadth

**Related Issues:**
- https://github.com/alsotoes/momo/issues/938 (R10: S3 lifecycle/versioning/notification/lock breadth)
- https://github.com/alsotoes/momo/issues/820 (Parent: S3 long tail)
- https://github.com/alsotoes/momo/issues/928 (Parent: Production readiness roadmap)

## Why

Momo's S3 gateway implements core data plane (Put/Get/Head/Delete/List/Copy/Multipart) but lacks the **bucket configuration** and **object lifecycle** features that production S3 clients (Veeam, restic, rclone, duplicati, AWS CLI advanced workflows) expect. The gaps are ranked by client impact:

1. **Versioning** (`?versioning`, `?versions`) — critical for backup tools (Veeam/restic) that rely on object versions for point-in-time recovery
2. **Lifecycle** (`?lifecycle`) — automated transitions/expirations; needed for cost control
3. **Notification** (`?notification`) — event-driven workflows (Lambda/SQS/SNS equivalents)
4. **Object Lock** (`?object-lock`) — compliance/retention (WORM)
5. **Bucket ACL/Policy** — already honest 501; defer to R8 multi-tenancy
3. **CORS, Website, Logging, Tagging, Replication config** — lower client impact

## What Changes

### Versioning (`?versioning`, `?versions`)
- Enable/disable/suspend versioning per bucket (`PUT ?versioning` with `<VersioningConfiguration><Status>Enabled|Suspended</Status></VersioningConfiguration>`)
- `GET ?versions` returns all versions (including delete markers) with `VersionId`, `IsLatest`, `LastModified`
- `GET Object` with `versionId` query param retrieves specific version
- `DELETE` without `versionId` creates delete marker; with `versionId` permanently deletes that version
- Storage: versioned objects stored as separate CAS blobs with version ID in metadata

### Lifecycle (`?lifecycle`)
- `PUT ?lifecycle` with rules: `<Rule><ID>...</ID><Filter><Prefix>...</Prefix></Filter><Status>Enabled</Status><Transition><Days>30</Days><StorageClass>GLACIER</StorageClass></Transition><Expiration><Days>365</Days></Expiration></Rule>`
- Background worker evaluates rules daily; executes transitions (copy to cold storage class) and expirations (delete object/versions)
- Config: `[s3] lifecycle_worker_interval` (default `24h`)

### Notification (`?notification`)
- `PUT ?notification` with topic config: `<TopicConfiguration><Topic>arn:...</Topic><Event>s3:ObjectCreated:*</Event><Filter>...</Filter></TopicConfiguration>`
- Supported events: `s3:ObjectCreated:*`, `s3:ObjectRemoved:*`, `s3:ObjectRestore:*`, `s3:Replication:*`
- Delivery: HTTP POST to configured endpoint (webhook); retry with exponential backoff
- Config: `[s3] notification_worker_concurrency`, `notification_retry_max`

### Object Lock (`?object-lock`)
- Bucket-level: `PUT ?object-lock` with `<ObjectLockEnabled>Enabled</ObjectLockEnabled>` (only at bucket creation)
- Object-level: `PUT Object` with `x-amz-object-lock-mode` (GOVERNANCE/COMPLIANCE), `x-amz-object-lock-retain-until-date`, `x-amz-object-lock-legal-hold`
- `GET Object` returns `x-amz-object-lock-mode`, `x-amz-object-lock-retain-until-date`, `x-amz-object-lock-legal-hold`
- Enforcement: WORM — overwrite/delete denied during retention; legal hold overrides

## Non-Goals

- Bucket ACL/Policy (deferred to R8 multi-tenancy)
- CORS (`?cors`), Website (`?website`), Logging (`?logging`), Tagging (`?tagging`), Replication config (`?replication`) — lower client impact
- S3 Select (`?select`), Analytics (`?analytics`), Metrics (`?metrics`), Intelligent Tiering — niche

## Impact

- **S3 Compatibility:** Enables Veeam/restic/duplicati/aws-cli advanced workflows
- **Storage:** Versioning increases blob count; lifecycle manages growth
- **Operations:** Lifecycle worker runs daily; notification worker async
- **Config:** New `[s3]` keys: `lifecycle_worker_interval`, `notification_worker_concurrency`, `notification_retry_max`
- **Metadata:** Versioned objects stored with version ID in `object_meta`