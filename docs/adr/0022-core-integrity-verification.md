# 0022-core-integrity-verification

## Status
Proposed

## Confidence
High

## Context
Integrity today is split across layers with inconsistent reach. `Hash` (SHA-256)
is the authoritative content-address and is verified in the shared `getFile`
ingest path for every surface — protocol-agnostic and correct. But additive
`x-amz-checksum-*` verification lives in the S3 surface adapter
(`transport.ChecksumFinalizer`), and SIP-verified digests are not re-checked at
replication hops or on retrieval. To make integrity a first-class property of
the **storage/ingest core** — independent of which client protocol delivers
bytes (commodity protocol, no lock-in) — we need a protocol-agnostic integrity
contract and a central verifier. Any new surface then inherits identical
guarantees without touching shared logic.

## Decision
- protocol-agnostic integrity contract: The system SHALL model additive integrity digests in `common.FileMetadata` as a list of `ChecksumRef{Algo, Value}` alongside the authoritative SHA-256 `Hash`. `Hash` SHALL remain the sole content-addressable identifier; `Checksums` are additive and MUST NOT be independently addressable.
- centralized ingest verification: The system SHALL verify every supplied additive checksum in the shared ingest path (`getFile`/store), independent of the ingress surface, and reject a mismatch by not persisting the object.
- replication hop re-verification: The system SHALL re-verify an additive checksum at every replication hop that receives object bytes, so integrity holds end-to-end across a chain/splay fan-out.
- surface adapters: The system SHALL keep client-protocol specifics (e.g. S3 `x-amz-checksum-*` header parse/arm/encode) inside surface adapters, mapping them onto the core contract, not into shared logic. ## UNCHANGED Behavior - SHA-256 `Hash` remains the content-address + dedup key; ETag unchanged. - Fixed momo wire framing fields (`Name`/`Hash`/`Size`/`RemotePath`/`ModTime`) unchanged; checksum extras are additive only. - aws-chunked trailing-checksum form (`x-amz-trailer`) still unsupported. - Per-`UploadPart` ...

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Partial
- **Docs**: Planned
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/core-integrity-verification/
- Blog: docs/blog/posts/...md
