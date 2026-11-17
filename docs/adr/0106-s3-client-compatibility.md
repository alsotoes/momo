# 0106-s3-client-compatibility

## Status
Proposed

## Confidence
Low

## Context
The gateway's S3 surface is verified with hand-written curl requests and unit
tests, which encode our own understanding of the protocol. Two real defects
reached `master` because of that gap: an unsigned PUT that reset the connection
(#1346) and a path-style PUT whose object could never be read back (#1348). Both
are trivial for a standard S3 client to expose.

## Decision
- Round-Trip With A Standard Client: CI SHALL drive a multi-node cluster with aws-cli and verify that an object a client writes can be read back byte-for-byte.
- Skips Without The Client: The check SHALL skip cleanly when aws-cli is unavailable instead of failing a local run.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1350
- PR: 
- Spec: `openspec/changes/s3-client-compatibility/`
- Blog: 

