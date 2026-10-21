# Spec: Bolt — zero-allocation SigV4 canonical header construction

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1195

## Requirements

### SIGV4-A1: Allocation-free canonical header construction

**Requirement:** `buildCanonicalHeaders` MUST construct the canonical-headers
block without heap allocation for a `SignedHeaders` list that fits the stack
buffer.

**Scenario: A typical S3 SignedHeaders list is canonicalized**

Given a request whose `SignedHeaders` is `host;x-amz-content-sha256;x-amz-date`,
When `buildCanonicalHeaders` runs,
Then the header-name list is parsed via a `strings.IndexByte` scan into a
stack-allocated buffer (no `strings.Split`),
And header values are resolved without `net/textproto.CanonicalMIMEHeaderKey`
(no `req.Header.Get`).

### SIGV4-A2: Byte-identical output for conformant inputs

**Requirement:** For every AWS-conformant (lowercase) `SignedHeaders` list and
present headers, the canonical output MUST be byte-identical to the legacy
implementation.

**Scenario: Golden equivalence holds**

Given the legacy implementation as an oracle,
When both implementations canonicalize the same conformant request,
Then their outputs are byte-identical (verified by
`TestBuildCanonicalHeaders_GoldenEquivalence`).

### SIGV4-A3: Fail-closed on malformed input

**Requirement:** A signed header that is absent from the request, or a
`SignedHeaders` list exceeding the stack buffer capacity, MUST be rejected with
a POSIX error rather than producing a broken canonical request.

**Scenario: Absent signed header**

Given `SignedHeaders` lists `x-absent` and the request has no such header,
When `buildCanonicalHeaders` runs,
Then it returns `syscall.EBADMSG`.

**Scenario: Oversized signed-header list**

Given a `SignedHeaders` list longer than the stack buffer capacity,
When `buildCanonicalHeaders` runs,
Then it fails closed with an error (Rule 32 bound).

### SIGV4-A4: Panic safety

**Requirement:** A panic during canonicalization MUST be recovered and surfaced
as `syscall.EIO` (Rules 37/43), never crashing the request handler.

**Scenario: Recovered panic**

Given a panic inside `buildCanonicalHeaders`,
When the deferred recovery runs,
Then the function returns `syscall.EIO` and the connection is not leaked.
