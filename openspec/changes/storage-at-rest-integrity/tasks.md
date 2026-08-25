# Tasks: Storage at-rest integrity — content-hash verify on read + background scrub (#924)

## 1. Streaming content-hash helper (`src/common`)
- [x] Add `HashReader(r io.Reader) (string, error)` to `common/hash.go` streaming
      SHA-256 through a fixed-size buffer (bounded memory)
- [x] Unit test: `HashReader` digest equals `HashBytes` for the same content

## 2. Verify-on-read (`src/storage`)
- [x] Add verifying `io.Reader`/`io.ReadCloser` wrappers in `storage.go` that
      recompute SHA-256 and assert the key at EOF (mismatch →
      `ErrIntegrityMismatch` + `syscall.EBADMSG`, no corrupt bytes served)
- [x] Apply wrapper in `CASStore.Get` when `VerifyOnRead` is enabled
- [x] Add `VerifyOnRead bool` to `CASStore`; default enable in `factory.go`

## 3. Background scrub (`src/storage`)
- [x] Add `ScrubConfig` + `DefaultScrubConfig` (interval 3600s)
- [x] Add `StartScrub` (sync.Once-guarded, mirrors `StartGC`) + `scrubLoop` +
      `runScrub`, cancellable via `scrubDone`/`scrubWG`
- [x] Quarantine path: iterate referenced blobs, re-read + re-hash via
      `BlobStore.GetBlob`, remove corrupt blob content + object metadata
      (explicit `ENOENT` on later reads); do not hold `s.mu` across blob I/O
- [x] Close integration: stop scrub in `CASStore.Close` (goleak-safe)

## 4. Config wiring
- [x] Add `ScrubInterval int`, `VerifyOnRead bool` to `ConfigurationStorage`
- [x] Load `scrub_interval` (default 3600) and `verify_on_read` (default true)
      in `loadStorageConfig`; defaults in `defaultStorageConfig`
- [x] Start scrub with configured interval in `factory.go`

## 5. Tests (`src/storage`)
- [x] Verify-on-read: valid blob serves to EOF; corrupted blob errors with
      `ErrIntegrityMismatch`+`EBADMSG`; gated off when `VerifyOnRead=false`
- [x] Scrub: healthy store round-trip (no quarantine); corrupted blob
      quarantined → read `ENOENT`
- [x] Panic-recover + goleak: store close stops scrub goroutine
- [x] Config: defaults and overrides for `scrub_interval` / `verify_on_read`

## 6. Validation + docs
- [x] `go fmt ./...`, `go vet ./...`, `go build ./...`, `go test ./...`
- [x] `go test -race ./...` on storage + common
- [x] `go work sync` + `go work vendor` parity
- [x] Docs: config sample + storage docs updated (Rule 27)
