# Change: Core Integrity Verification (protocol-agnostic)
**Related Issues:**
- https://github.com/alsotoes/momo/issues/903

## Why
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

## What Changes
- **Add a protocol-agnostic integrity contract** to `common.FileMetadata`:
  additive `Checksums []ChecksumRef{Algo, Value}`, layered on the authoritative
  SHA-256 `Hash` (content-address unchanged; additive only).
- **Centralize verification at the store/ingest layer**, so all surfaces
  (s3-*, momo-tcp, momo-quic) verify identically; every replication hop
  re-verifies on receive; retrieval (`store.Get`) can detect stale/bit-rot.
- **Surfaces become adapters**: S3 `x-amz-checksum-*` maps into `ChecksumRef`;
  the S3-specific parse/arm/finalize mechanism (`parseChecksum`,
  `FinalizeIntegrityChecksum`) moves into the adapter and out of shared logic.
- **Reuse the `ChecksumFinalizer` seam** introduced by #902 as the extension
  point, generalizing it beyond S3.
- Verification runs only when a client supplies a checksum (inert otherwise) —
  no double-hash on the common path.

## Additive-only wire/metadata
The fixed momo wire framing fields (`Name`/`Hash`/`Size`/`RemotePath`/`ModTime`)
remain authoritative and unchanged. `S3Headers`/`Checksums` are additive extras
carried between peers only by the existing optional `X-Momo-S3-Meta` header;
peers that ignore extras store/echo nothing and are unaffected.

## Non-Goals
- Changing the dedup content-address: SHA-256 `Hash` stays the address;
  checksums are never independently addressable.
- Any breaking protocol change or storage-schema rewrite.
- Per-`UploadPart` per-part checksums (final assembled checksum verification only).
- Enabled-by-default re-verification on every `store.Get` in all deployments
  (opt-in bit-rot checking only, to avoid perf regression).

## Scope
Standalone integrity-architecture item (issue #903). Independent of the #820
umbrella (P1/P3/P4/P5 tiers are tracked separately). Depends on #902 (P2
checksums merged), which provides the `ChecksumFinalizer` extension point.

## Impacted areas
- `common/struct.go` (`FileMetadata`: `Checksums` field), `common` marshaling.
- `storage/storage.go` CAS store: optional verifier call on `Put`/`Get`;
  sidecar persistence of `Checksums`.
- `server/file.go` (`getFile`): invoke the generic verifier.
- `transport/communicator.go` (`ChecksumFinalizer` seam), `transport/s3_communicator.go`
  (adapter mapping `x-amz-checksum-*` → `ChecksumRef`).
- `docs/COMPATIBILITY.md`, `docs/ARCHITECTURE.md` parity updates.
- Tests: surface adapters, ingest verifier, replication re-verify, bit-rot `Get`.
