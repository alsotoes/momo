# 0021-bolt-s3-copyresult-time-alloc

## Status
Accepted

## Confidence
High

## Context
`FormatCopyObjectResultXML` calls `formatLastModified`, which internally invokes
`time.Format()`. `.Format()` returns a dynamically allocated string, so every S3
Copy operation response pays one heap allocation on a hot path — adding GC
pressure during sustained Copy workloads. The standard-library idiom
`AppendFormat` + a stack-allocated `[32]byte` scratch buffer writes directly
into the response `bytes.Buffer` with zero heap allocations.

## Decision
- allocation-free LastModified formatting: `FormatCopyObjectResultXML` SHALL render the `LastModified` element using `time.AppendFormat` with a stack-allocated `[32]byte` scratch buffer written directly into the response `bytes.Buffer`, without calling `time.Format()` and without any dynamic string allocation for the timestamp. ## UNCHANGED Behavior - `formatLastModified` remains in use at its other call sites. - XML structure, escaping, and ETag handling in the CopyObject result are unchanged. - CopyObject request semantics and S3 respo...

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/bolt-s3-copyresult-time-alloc/
- Blog: docs/blog/posts/...md
