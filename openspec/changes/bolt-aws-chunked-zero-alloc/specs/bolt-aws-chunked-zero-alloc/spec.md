# Spec: Bolt — zero-allocation AWS chunked header parsing

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1113

## Requirements

### BAC-A1: Allocation-free chunk-header parsing

**Requirement:** `parseAWSChunkHeader` MUST parse an `aws-chunked` chunk-header
line without heap allocation.

**Scenario: A signed chunk header is parsed**

Given the chunk-header line `10000;chunk-signature=<sig>`,
When `parseAWSChunkHeader` parses it,
Then it returns size `0x10000`, the signature, and no error,
And no heap allocation is performed (verified by `BenchmarkParseAWSChunkHeader`
reporting `0 allocs/op`).

### BAC-A2: Parse semantics unchanged

**Requirement:** The refactor MUST be behavior-preserving for every input
accepted or rejected by the previous `strings.Split` implementation.

**Scenario: Field and edge-case handling is unchanged**

Given any chunk-header line,
When it is parsed before and after the change,
Then the returned size, signature, and error are identical,
Including: a line with no `;`, a trailing `;`, an empty field (`;;`),
extension fields before/after the signature, surrounding whitespace around the
size, multiple `chunk-signature=` fields (last wins), an empty signature, an
empty size, a non-hex size, and a negative size.

### BAC-A3: Signature length bound preserved

**Requirement:** A signature longer than 64 bytes MUST still be rejected.

**Scenario: Oversized signature is rejected**

Given a chunk-signature field whose value exceeds 64 bytes,
When `parseAWSChunkHeader` parses the line,
Then it returns an error (fail-closed, `syscall.EBADMSG`).

### BAC-A4: No allocation regression (benchmark gate)

**Requirement:** The optimization MUST remove the per-chunk parsing allocation.

**Scenario: Benchmark measures allocation count**

Given the `BenchmarkParseAWSChunkHeader` microbenchmark,
When it runs with `-benchmem`,
Then the result reports `0 allocs/op` (vs `1 alloc/op`, 32 B, for the previous
`strings.Split` + `strings.TrimPrefix` implementation).

## Non-Goals

- No change to the aws-chunked state machine or signature verification.
- No wire/protocol or configuration change.
