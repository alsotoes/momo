# 0110-s3-trailer-checksum-verification

## Status
Proposed

## Confidence
Low

## Context
A client streaming an upload with aws-chunked can assert integrity in a header or
in a **trailer** at the end of the body. The gateway accepted the trailer form but
discarded the value, so a corrupted stream was stored silently: the object is
keyed by the SHA-256 of the bytes received, and nothing compared those bytes with
what the client meant to send.

`COMPATIBILITY.md` recorded this as a known ceiling.

## Decision
- Verify A Trailing Checksum: A trailing `x-amz-checksum-<algo>` SHALL be verified over the decoded bytes before the payload is stored.
- Bounded And Strict Parsing: The trailing-header block SHALL be bounded and validated so it cannot be used to inject bytes.
- Parts Are Covered: A multipart part body SHALL receive the same verification.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1363
- PR: 
- Spec: `openspec/changes/s3-trailer-checksum-verification/`
- Blog: 

