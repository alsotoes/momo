# MomoFS Mount — User Guide

The momofs FUSE transport (`-imp fs`) exposes a momo daemon's content-addressed
store as a POSIX filesystem via FUSE. It was implemented as the transport layer
for the R4 momofs core (openspec/changes/r4-momofs/, #932) and ratifies the
[IMPLEMENTATION.md](IMPLEMENTATION.md) "2.3 FUSE Mount" section.

## Requirements

- Linux with FUSE support (`/dev/fuse` present, `fusermount`/`fusermount3`
  installed). This is a client-side adapter; the store backing a mount is the
  same `[storage]`-configured CAS store a daemon uses.
- The data directory must be writable (defaults to the selected daemon's
  `data=` entry; override with `-fs-data`).

## Mounting

```
bin/momo -imp fs -id 0 -fs-mount /mnt/momo [-fs-data /var/lib/momo/fs]
```

| Flag | Meaning |
|------|---------|
| `-imp fs` | Select the momofs FUSE mount impersonation |
| `-id N` | Which `[daemon.N]` from `conf/momo.conf` provides the storage backend (default `0`) |
| `-fs-mount PATH` | Mount point (must exist; required) |
| `-fs-data PATH` | Optional data-directory override for the backing store |
| `-config PATH` | Config path (default `conf/momo.conf`) |

The process serves until it receives `SIGINT`/`SIGTERM`, then unmounts and
exits. Mount options such as `allow_other`, inode/attr caching hints, and
right-size block reads are the natural follow-ups (see ROADMAP).

## What you get

- **Directories** = content-addressed JSON manifests (atomic, versioned by
  content hash); mode/uid/gid/size/mtime are POSIX metadata from the manifest.
- **Files** = store objects keyed by path identity (content dedup via hash);
  reads stream from the store, writes are buffered per open handle and
  materialized as one CAS blob on close/flush.
- **Rename** is atomic (manifest rewrite); **hardlinks** keep a store reference
  per live link so refcounts stay aligned with CAS GC.
- **Native ↔ mount consistency (R4-C3)**: objects written natively (S3/momo
  client) appear in the mount (subject to the store index TTL, default 1s),
  and mount writes are real store objects.

## Example

```sh
# start a momo daemon (or reuse an existing config + data dir)
bin/momo -imp server -id 0 &

# mount its store as a filesystem
mkdir -p /mnt/momo
bin/momo -imp fs -id 0 -fs-mount /mnt/momo

# POSIX use
echo hello > /mnt/momo/notes.txt
cat /mnt/momo/notes.txt
mkdir /mnt/momo/photos
cp -r ~/pics /mnt/momo/photos/

# write through the mount now visible via native S3/API (after index TTL)
bin/momo -imp client -id 0 -file notes.txt -remote-path notes.txt -id 0

# unmount on signal (Ctrl-C on the fs process)
```

## Consistency & caching

The mount FS reads manifests fresh on every op by default (`WithCacheTTL(0)`),
so reads are read-your-owns-writes. The backing-store object index used to
surface natively-written files is refreshed at most every
`WithIndexTTL` (default 1s) — the same window the core `momofs.FS` documents for
S3↔mount visibility. Set a `WithCacheTTL` for attr caching on top of the core
where stale-read tolerance is acceptable.

## Limitations / follow-ups

- **Byte-range writes** are implemented per-handle as a buffered view that
  materializes a whole new CAS blob on flush — correct, but whole-file-cost on
  large append-heavy workloads (see `tasks.md` "mmap/byte-range correctness").
- **POSIX locks / flock** are not yet exported (follow-up task).
- **Truncate (`ftruncate`)** size hints are accepted but truncation semantics
  land with the byte-range write follow-up.
- Remote (S3-backed) stores are supported for writes only when the data dir is
  local; the FUSE adapter itself is transport-agnostic.