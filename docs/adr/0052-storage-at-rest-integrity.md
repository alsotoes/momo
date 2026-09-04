# 0052-storage-at-rest-integrity

## Status
Accepted

## Confidence
High

## Context
Momo is content-addressed: every blob's identity is its SHA-256 content hash
(`common.HashBytes`/`HashFile`), and the storage key for a blob *is* that hash.
But the read path (`CASStore.Get`) streams blob bytes out to clients **without
ever re-deriving the hash**. If a blob is corrupted at rest (bit rot, torn
writes, partial S3 downloads), the corrupt bytes are served to clients silently.
The existing integrity surface is either additive/opt-in
(`VerifyChecksum`, issue #903) or in-band ingest-side (`getFile`), neither of
which detects a blob that no longer matches its own address key.

This change closes that gap with two complementary mechanisms, per recommendation
item 1 of the #820 production-hardening roadmap:

1. **Verify-on-read** — `CASStore.Get` re-derives the SHA-256 of the whole blob
   as it streams and, at EOF, asserts it equals the content-hash key. On
   mismatch it returns an integrity error instead of serving corrupt bytes. It
   is bounded-memory (streamed through a fixed buffer) and computationally
   outside the bbolt/`s.mu` read critical section (the caller drains the wrapped
   reader after `Get` returns).
2. **Background scrub** — a `StartScrub` goroutine that mirrors the GC loop
   (`gcLoop`): periodically it iterates the referenced blobs in the `objects`
   bucket, re-reads and re-hashes each through `BlobStore.GetBlob`, and
   quarantines any blob whose recomputed hash no longer matches its key (removes
   blob content + object metadata so the name fails explicitly with `ENOENT`
   rather than silently serving garbage). Both are defensive (panic-recovered),
   cancellable on store close (goleak-safe, mirroring `gcDone`/`gcWG`), and
   `sync.Once`-guarded.

Both are gated by new `[storage]` config keys: `verify_on_read` (default `true`)
and `scrub_interval` (seconds, default `3600`).

## Decision
- Verify-on-read failures are explicit: The system SHALL, when `verify_on_read` is enabled (default `true`), recompute the SHA-256 over the entire blob stream returned by `CASStore.Get` and, at EOF, assert it equals the blob's content-hash key. When it does not equal, reads SHALL fail with an error wrapping `common.ErrIntegrityMismatch` and `syscall.EBADMSG`; no corrupt bytes are served.
- Verify-on-read is configurable: The system SHALL accept a `[storage]` config key `verify_on_read` (boolean, default `true`) controlling whether `CASStore.Get` wraps blob streams with content-hash verification.
- Background scrub quarantines corrupt blobs: The system SHALL provide `CASStore.StartScrub` (call-safe at most once, mirroring `StartGC`) that periodically iterates referenced blobs in the `objects` bucket, re-reads and re-hashes each via `BlobStore.GetBlob`, and for any blob whose recomputed hash does not equal its key, quarantines it: removes the blob content and its object metadata so subsequent reads for names mapping to that hash fail with `syscall.ENOENT`. The pass SHALL be bounded-memory, panic-recovered, cancellable on store close,...
- Scrub interval is configurable: The system SHALL accept a `[storage]` config key `scrub_interval` (integer seconds, default `3600`) controlling how often the scrub pass runs.
- Content-hash streaming helper: The system SHALL expose `common.HashReader(r io.Reader)` returning the hex SHA-256 of `r` while streaming through a fixed-size buffer (bounded memory), mirroring `common.HashFile`.

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
- Spec: openspec/changes/storage-at-rest-integrity/
- Blog: docs/blog/posts/...md
