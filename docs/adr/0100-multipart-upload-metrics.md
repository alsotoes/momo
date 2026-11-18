# 0100-multipart-upload-metrics

## Status
Proposed

## Confidence
Low

## Context
Since #1358 multipart parts live on disk under `<data>/multipart-spill`, bounded
by `multipart_upload_ttl` since #1361. Neither the number of open uploads nor the
disk they hold was visible: "is this node's data dir growing from uploads or from
objects?" could only be answered by reading AUDIT lines, and a node filling its
disk with abandoned uploads looked exactly like normal object growth.

## Decision
- Report Open Uploads And Their Spill: The metrics endpoint SHALL report the number of open multipart uploads and the bytes their spooled parts hold.
- Scrape Is Cheap: 

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1369
- PR: 
- Spec: `openspec/changes/multipart-upload-metrics/`
- Blog: 

