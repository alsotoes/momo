# Proposal: Eliminate redundant time formatting allocations for AWS SigV4 datestamps

## Problem Statement

When constructing AWS SigV4 requests, both a full timestamp (`20060102T150405Z`) and a datestamp (`20060102`) are required. Currently, the code calls `time.Format` twice, passing different format strings to `time.Now().UTC()`. `time.Format` dynamically parses the format string and allocates a new string on the heap for each call. Calling it twice sequentially creates unnecessary garbage collection pressure and CPU overhead on hot paths for request signing.

## Proposed Solution

Extract the datestamp directly by slicing the first 8 characters of the already-formatted full timestamp (`amzDate[:8]`). Since the first 8 characters of the full timestamp are exactly the datestamp, this avoids the second `time.Format` call entirely.
