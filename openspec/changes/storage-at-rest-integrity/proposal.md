# Change: Storage at-rest integrity — content-hash verify on read + background scrub

**Related Issues:**
- https://github.com/alsotoes/momo/issues/924
- https://github.com/alsotoes/momo/issues/820 (roadmap)

## Why

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

## Non-goals

- No replication/erasure-coding or cross-replica healing. This detects and
  quarantines local corruption; re-replication is a separate roadmap item.
- No change to the additive checksum surface (`VerifyChecksum`, issue #903).
- No change to `GetBlobPath`/direct-file serving paths (follow-up), or to the
  write side.
