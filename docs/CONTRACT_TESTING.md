# TCP Contract Testing

This document describes the contract testing strategy for the Momo wire protocol, ensuring that protocol-level changes are detected before they reach production.

## Wire Protocol Contract

The Momo wire protocol has fixed-size fields that **must not change** without a protocol version bump. The contract tests in `src/server/contract_test.go` assert these exact byte-level contracts.

### Handshake (84 bytes)

```
|-----------------|-----------------|------|
|  AuthToken (64) | Timestamp (19)  | M (1)|
|-----------------|-----------------|------|
```

- **AuthToken**: 64-byte null-padded string
- **Timestamp**: 19-byte ASCII string (UnixNano)
- **Mode**: 1-byte ASCII character (`'0'`-`'3'`, `'L'`, `'D'`, `'G'`)

### Metadata (192 bytes)

```
|-----------------|------------------|-----------------|
|   Hash (64)     | File Name (64)   | File Size (64)  |
|-----------------|------------------|-----------------|
```

- **Hash**: 64-byte hexadecimal SHA-256 string
- **File Name**: 64-byte null-padded ASCII string
- **File Size**: 64-byte null-padded ASCII decimal string

### Status Code (1 byte)

- `1` = `MetadataStatusSendPayload` — client should send file payload
- `2` = `MetadataStatusSkipPayload` — server has content (CAS deduplication hit)

### ACK (4 bytes, fixed-length)

- `"ACK" + serverId byte` (raw byte value, not ASCII digits; e.g. `ACK\x00` for server 0) — server acknowledgment after successful file transfer. Fixed-length framing prevents leftover bytes from corrupting the next protocol message (issue #621).

### P2P RPC Framing (4-byte length prefix + body)

```
|-----------------|----------------|
|  Length (4)     |  Body (N)      |
|-----------------|----------------|
```

- **Length**: 4-byte big-endian uint32
- **Body**: `[Type (1)] [From (4)] [Payload (N-5)]`
- Minimum body length: 5 bytes
- Maximum body length: 1MB (1<<20)

## Contract Tests

| Test | What it asserts |
|------|-----------------|
| `TestContract_HandshakeLayout` | Handshake is exactly 84 bytes; `AuthTokenLength` is 64 |
| `TestContract_MetadataLayout` | Metadata is exactly 192 bytes |
| `TestContract_HandshakeRoundTrip` | Full handshake round-trip preserves token and mode |
| `TestContract_P2PRPCFraming` | 4-byte big-endian length prefix framing is correct |
| `TestContract_FileMetadataSizes` | Padded file name and size are exactly 64 bytes each |

## Running Contract Tests

```bash
go test -run TestContract -v ./src/server/...
```

## Adding New Contract Tests

When adding a new protocol feature:

1. Add a constant in `contract_test.go` documenting the exact wire size.
2. Write a test that sends and receives the message, asserting byte-level layout.
3. Update `docs/PROTOCOL.md` with the new message format.
4. Ensure the test passes with `-race` flag.
