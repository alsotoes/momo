# 0051-zero-crash-hardening

## Status
Accepted

## Confidence
High

## Context
Momo relies on custom wire protocols and manual byte-slice parsing to achieve its high-performance, zero-allocation goals. However, manual parsing of network data (e.g., extracting timestamps, filenames, and replication modes from raw byte streams) introduces significant risk. Malformed packets, intentional fuzzing, or unexpected null padding can trigger panics (e.g., out-of-bounds slice access), unhandled conversion errors (`strconv.Atoi`), or resource exhaustion (allocating massive slices based on malicious size headers). 

To ensure the Momo cluster remains highly available and resilient against both accidental misconfigurations and active denial-of-service (DoS) attempts, a massive refactoring of our data parsing logic is required.

## Decision
- Defensive Parsing Validation: All data parsed from external sources (network connections, configuration files) SHALL undergo strict boundary and format validation prior to type conversion or application logic execution.
- Bounded Resource Allocation: The system SHALL NOT allocate memory or disk buffers based solely on unvalidated sizes provided in network metadata.
- Fuzz-Tested Resilience: All critical parsing functions exposed to network data SHALL be verified using Go's native fuzzing framework (`testing.F`) to ensure immunity against panics from unexpected byte combinations.
- Goroutine Panic Recovery: Critical long-running loops and dynamically spawned network handlers SHALL implement panic recovery to prevent total application failure.
- Strict Performance Threshold: The addition of defensive checks and panic recovery mechanisms SHALL NOT incur a performance penalty greater than 5% as measured by the `make benchmark` suite.

## Consequences


## Alternatives Considered
None documented.

## Confidence
High

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/zero-crash-hardening/
- Blog: docs/blog/posts/...md
