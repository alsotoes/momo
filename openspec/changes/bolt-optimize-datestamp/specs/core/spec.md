# Spec: Datestamp Allocation Optimization

## Purpose

To eliminate redundant string allocations and CPU overhead caused by calling `time.Format` twice when generating timestamps for AWS SigV4 requests.

## Requirements

1. The system SHALL derive the `dateStamp` by slicing the first 8 bytes of the `amzDate` string (the full timestamp) instead of calling `time.Format` a second time.
2. Inline comments MUST explain the optimization (e.g., `// Derive datestamp directly from amzDate to eliminate redundant time parsing and string allocations`).

## Consequences

- Reduced heap allocations per request on S3 communication layers.
- Lower GC pressure and CPU overhead.

## GitHub Issue URL

https://github.com/alsotoes/momo/issues/1065
