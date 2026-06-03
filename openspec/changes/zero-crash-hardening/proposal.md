# Change: Zero-Crash Hardening
**Related Issue:** #135

## Why
Momo relies on custom wire protocols and manual byte-slice parsing to achieve its high-performance, zero-allocation goals. However, manual parsing of network data (e.g., extracting timestamps, filenames, and replication modes from raw byte streams) introduces significant risk. Malformed packets, intentional fuzzing, or unexpected null padding can trigger panics (e.g., out-of-bounds slice access), unhandled conversion errors (`strconv.Atoi`), or resource exhaustion (allocating massive slices based on malicious size headers). 

To ensure the Momo cluster remains highly available and resilient against both accidental misconfigurations and active denial-of-service (DoS) attempts, a massive refactoring of our data parsing logic is required.

### Architectural Rationale: Defensive Processing Pipeline
The system must transition to a **Defensive Processing Pipeline**:
1.  **Validate Before Convert:** No data from an untrusted source (network or configuration) is passed to a conversion function (`strconv`, `json.Unmarshal`) or used in an allocation without prior boundary and character-set validation.
2.  **Graceful Degradation:** A malformed payload must *only* terminate the offending connection or request, never the overarching daemon or metrics loop.
3.  **No Unbounded Allocations:** Slice creation or file chunking must adhere to strict, pre-defined maximum limits, regardless of the values declared in incoming metadata.
4.  **Strict Performance Bound:** All defensive checks and recovery mechanisms must be highly optimized. As per project steering rules, the refactoring **MUST NOT introduce a performance degradation larger than 5%** against the baseline benchmarks per pull request.

## What Changes
- Refactor all integer and string parsing functions (`parsePaddedIntFast`, `GetMetadata`, replication mode parsing) to include strict bounds checking and robust error handling.
- Audit and replace unsafe uses of `strconv.Atoi` on raw network buffers with safer, bounded alternatives.
- Implement comprehensive Fuzz Testing across all network boundaries to aggressively hunt for panic conditions.
- Ensure all goroutines spawned by the server loops contain panic recovery mechanisms where appropriate, wrapping unexpected crashes into logged `AUDIT` errors rather than daemon termination.

## Impact
- Affected specs: `security`, `stability`
- Affected code: `src/server/file.go`, `src/common/replication.go`, `src/common/config.go`, `src/server/server.go`
