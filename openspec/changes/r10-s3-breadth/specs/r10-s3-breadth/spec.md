> GitHub Issue URL: https://github.com/alsotoes/momo/issues/938

# Spec: R10 — S3 Lifecycle, Versioning, Notification, Object Lock Breadth

## Requirements

### R10-V1: Versioning

**Requirement:** Bucket versioning MUST be controllable per bucket with standard S3 semantics.

**Scenario: Enable versioning**

Given a bucket exists,
When `PUT ?versioning` with `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`,
Then the bucket is versioned, and subsequent `PUT`s create new versions.

**Scenario: List versions**

Given a versioned bucket with multiple versions,
When `GET ?versions` is requested,
Then all versions (including delete markers) are returned with `VersionId`, `IsLatest`, `LastModified`.

**Scenario: Get specific version**

Given a versioned object,
When `GET Object` with `versionId` query param is requested,
Then that specific version's content is returned.

**Scenario: Delete with/without versionId**

Given a versioned object,
When `DELETE` without `versionId`,
Then a delete marker is created (object appears deleted).
When `DELETE` with `versionId`,
Then that specific version is permanently deleted.

### R10-L1: Lifecycle

**Requirement:** Bucket lifecycle rules MUST automate transitions and expirations.

**Scenario: Transition rule**

Given `PUT ?lifecycle` with a rule: `<Rule><Filter><Prefix>logs/</Prefix></Filter><Status>Enabled</Status><Transition><Days>30</Days><StorageClass>GLACIER</StorageClass></Transition></Rule>`,
When the daily worker runs after 30 days,
Then matching objects are transitioned to the cold storage class.

**Scenario: Expiration rule**

Given `PUT ?lifecycle` with `<Expiration><Days>365</Days></Expiration>`,
When the daily worker runs after 365 days,
Then matching objects are expired (delete marker created if versioned; permanently deleted if not).

**Scenario: Multiple rules**

Given multiple lifecycle rules with different prefixes/filters,
When the worker runs,
Then all enabled rules are evaluated independently.

### R10-N1: Notification

**Requirement:** Bucket notification configurations MUST deliver event-driven webhooks.

**Scenario: Configure notification**

Given `PUT ?notification` with `<TopicConfiguration><Topic>http://webhook:9000</Topic><Event>s3:ObjectCreated:*</Event></TopicConfiguration>`,
When an object is created,
Then an HTTP POST is delivered to the webhook with the event payload.

**Scenario: Event filtering**

Given a notification config with `<Filter><S3Key><FilterRule><Name>prefix</Name><Value>uploads/</Value></FilterRule></S3Key></Filter>`,
When an event occurs,
Then the webhook is only triggered for matching keys.

**Scenario: Retry with backoff**

Given a webhook returns 5xx,
When delivery fails,
Then retries occur with exponential backoff up to `[s3] notification_retry_max`.

### R10-LK1: Object Lock

**Requirement:** Object Lock MUST enforce WORM retention and legal hold.

**Scenario: Bucket-level lock enablement**

Given `PUT ?object-lock` with `<ObjectLockEnabled>Enabled</ObjectLockEnabled>` at bucket creation,
When the bucket is created,
Then Object Lock is enabled and cannot be disabled.

**Scenario: Object-level retention**

Given `PUT Object` with `x-amz-object-lock-mode: COMPLIANCE` and `x-amz-object-lock-retain-until-date: 2026-12-31T00:00:00Z`,
When the object is stored,
Then overwrite/delete is denied until the retention date.

**Scenario: Legal hold**

Given an object with `x-amz-object-lock-legal-hold: ON`,
When overwrite/delete is attempted,
Then the request is denied regardless of retention date.

## Non-Goals

- Bucket ACL/Policy (deferred to R8 multi-tenancy)
- CORS (`?cors`), Website (`?website`), Logging (`?logging`), Tagging (`?tagging`), Replication config (`?replication`) — lower client impact
- S3 Select (`?select`), Analytics (`?analytics`), Metrics (`?metrics`), Intelligent Tiering — niche