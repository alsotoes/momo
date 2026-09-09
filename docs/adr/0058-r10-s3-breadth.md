# 0058-r10-s3-breadth

## Status
Proposed

## Confidence
Low

## Context
Momo's S3 gateway implements core data plane (Put/Get/Head/Delete/List/Copy/Multipart) but lacks the **bucket configuration** and **object lifecycle** features that production S3 clients (Veeam, restic, rclone, duplicati, AWS CLI advanced workflows) expect. The gaps are ranked by client impact:

1. **Versioning** (`?versioning`, `?versions`) — critical for backup tools (Veeam/restic) that rely on object versions for point-in-time recovery
2. **Lifecycle** (`?lifecycle`) — automated transitions/expirations; needed for cost control
3. **Notification** (`?notification`) — event-driven workflows (Lambda/SQS/SNS equivalents)
4. **Object Lock** (`?object-lock`) — compliance/retention (WORM)
5. **Bucket ACL/Policy** — already honest 501; defer to R8 multi-tenancy
3. **CORS, Website, Logging, Tagging, Replication config** — lower client impact

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #938
- PR: 
- Spec: `openspec/changes/r10-s3-breadth/`
- Blog: 

