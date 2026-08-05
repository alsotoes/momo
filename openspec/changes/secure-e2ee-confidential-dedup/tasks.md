# Secure E2EE — Confidential Dedup via Threshold OPRF + Crypto Hardening

## Phase A: Crypto Hardening

### A1. Streaming nonce non-reuse
- [x] A1.1 Rewrite `EncryptStream` in `src/crypto/streaming.go` to seed the per-stream nonce with 8 random bytes (`crypto/rand`) and put the chunk counter in `nonce[8:12]`. Bump `StreamVersion = 2`.
- [x] A1.2 Update `DecryptStream` to build the nonce identically and validate the header version.
- [x] A1.3 Add tests: two streams same key → distinct first-chunk nonces; legacy `StreamVersion=1` rejected; round-trip still works.

### A2. Domain-separated HKDF
- [x] A2.1 Rewrite `DeriveKey(master, tenant, context)` in `src/crypto/crypto.go` to build length-encoded `info = label(len)|label|tenant(len)|tenant|context(len)|context`.
- [x] A2.2 Add domain-label helpers (`"momo/token"`, `"momo/content"`, `"momo/atrest"`, `"momo/oprf"`).
- [x] A2.3 Add tests: `DeriveKey(K,"ab","c") != DeriveKey(K,"a","bc")`; distinct domain labels → distinct keys; determinism.

### A3. Tenant-derived content key + streaming client
- [x] A3.1 `src/client/client.go`: derive `tenantKey = DeriveKey(masterKey, tenant, "momo/content")` and build the cipher from it (not raw master).
- [x] A3.2 Replace the `bytes.Buffer` full-file buffer with a streaming pipe (chunk-bounded memory) on upload.
- [x] A3.3 `src/server/server.go` / `src/storage/factory.go`: SSE store key = `DeriveKey(masterKey, tenant, "momo/atrest")`.
- [x] A3.4 `src/storage/encrypted_blobstore.go`: use the at-rest derived key.
- [x] A3.5 Add tests: tenant isolation (tenant A key cannot decrypt tenant B content), streaming client memory-bound.

### A4. Remove convergent encryption
- [x] A4.1 Delete `src/crypto/convergent.go`.
- [x] A4.2 Remove `TestConvergentEncrypt*` from `crypto_test.go` and `BenchmarkConvergentEncrypt` from `bench_test.go`.
- [x] A4.3 Verify `go build ./... && go test ./...` pass with convergent code gone.

## Phase B: Threshold OPRF + P2P OPRF RPC

### B1. OPRF crypto package
- [x] B1.1 Add `github.com/bytemare/crypto` to `src/crypto/go.mod`; run `go work sync` + `go work vendor` (Rule 25).
- [x] B1.2 New `src/crypto/oprf.go`: `OPRFBlind(input) (blinded, blind, err)`, `OPRFEvaluateShare(shareScalar, blinded)`, `OPRFCombineUnblind(evaluations, blind) (key, error)` — fail if `len(evaluations) < t`.
- [x] B1.3 Add tests: OPRF round-trip determinism, threshold fail-closed, tamper detection.

### B2. OPRF config
- [x] B2.1 Add `oprf_enabled`, `oprf_threshold` to `ConfigurationGlobal` (`src/common/struct.go`) and validate in `loadGlobalConfig` (`src/common/config.go`).

### B3. P2P OPRF RPC
- [x] B3.1 Add P2P message types for OPRF eval request/response in `src/p2p/types.go` (e.g., `MsgOPRFEvalRequest`, `MsgOPRFEvalResponse`).
- [x] B3.2 Add OPRF RPC routing in `src/p2p/gossip.go` `HandleRPC` mirroring `ScatterGather.HandleRPC`.
- [x] B3.3 Add tests: OPRF eval request/response round-trip over `net.Pipe`-backed transport.

### B4. Client integration
- [x] B4.1 On upload: compute `tag = H(plaintext)`, `(blinded, blind)` via OPRF, dispatch to `t` daemons, combine → content key.
- [x] B4.2 Encrypt content with the content/tenant key streaming; store CAS hash = `tag` (dedup preserved). Fail-closed if `#evaluations < t`.
- [x] B4.3 Add integration tests: E2EE dedup (same content, different names → one blob), fail-closed with fewer than `t` daemons.

## Phase C: Closeout

- [x] C1. Rewrite `docs/PROTOCOL.md` E2EE + OPRF sections (Rule 27).
- [x] C2. Update `docs/CONFIGURATION.md` (`oprf_enabled`, `oprf_threshold`).
- [x] C3. Update `docs/ARCHITECTURE.md` encryption/deoopy matrix + `README.md` parity.
- [x] C4. Run `go build ./...`, `go test -race ./...`, `go vet ./...`, `gofmt`.
- [x] C5. Run benchmark suite; document overhead in `docs/PERFORMANCE.md`.
- [x] C6. Verify backward compat: all tests pass with `encryption_enabled = false`.

## Steering-Rule Compliance Notes

- **Rule 10/37/43:** OPRF RPC handlers use unified panic recovery + POSIX error mapping.
- **Rule 25:** vendor parity maintained (`go work sync && go work vendor`, no diff).
- **Rule 27:** any config/protocol change updates `docs/`.
- **Rule 33:** feature must work across `momo-tcp`/`momo-quic` (protocol feature parity).
- **Rule 40:** network tests bind ephemeral ports (`127.0.0.1:0`).

---