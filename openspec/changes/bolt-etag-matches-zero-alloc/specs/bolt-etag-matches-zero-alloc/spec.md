# Spec: Bolt — zero-allocation ETag header parsing

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1133

## Requirements

### ETAG-A1: Allocation-free ETag list parsing

**Requirement:** `etagMatches` MUST parse a comma-separated entity-tag list
without heap allocation.

**Scenario: A multi-tag If-Match header is parsed**

Given the header `"aaa111", "bbb222", "ccc333", W/"ddd444", "target"`,
When `etagMatches` parses it,
Then it returns true for `target`,
And no heap allocation is performed (verified by `BenchmarkEtagMatches`
reporting `0 allocs/op`).

### ETAG-A2: Parse semantics unchanged

**Requirement:** The refactor MUST be behavior-preserving for every input
accepted or rejected by the previous `strings.Split` implementation (for a
non-empty entity tag).

**Scenario: Field and edge-case handling is unchanged**

Given any entity-tag list header and a non-empty etag,
When it is parsed before and after the change,
Then the boolean result is identical, including: quoted and unquoted tags,
`W/` weak tags (with or without an intervening space), the `*` wildcard
(alone or within a list), leading/trailing whitespace, a trailing comma, a tag
absent from the list, and an empty header.

### ETAG-A3: Wildcard and weak comparison preserved

**Requirement:** `*` MUST match every object, and a `W/` weak tag MUST compare
equal to the same raw hash.

**Scenario: Wildcard matches**

Given the header `*`,
When `etagMatches` is called with any etag,
Then it returns true.

**Scenario: Weak tag matches**

Given the header `W/"etag123"`,
When `etagMatches` is called with `etag123`,
Then it returns true.

### ETAG-A4: No allocation regression (benchmark gate)

**Requirement:** The optimization MUST remove the per-request parsing
allocation.

**Scenario: Benchmark measures allocation count**

Given the `BenchmarkEtagMatches` microbenchmark,
When it runs with `-benchmem`,
Then the result reports `0 allocs/op` (vs `1 alloc/op`, 80 B, for the previous
`strings.Split` implementation, measured by `BenchmarkEtagMatchesSplit`).

## Non-Goals

- No change to the conditional-request decision logic or its callers.
- No wire/protocol or configuration change.
