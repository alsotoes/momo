# Spec: Bolt — eliminate time.Format string allocation in S3 ListParts

## Requirements

### BL-A1: Allocation-free timestamp formatting in handleListParts

**Requirement:** The `handleListParts` handler in `S3Communicator` MUST render
the `LastModified` timestamp for each `<Part>` element without a heap
allocation.

**Scenario: ListParts response contains an upload with one or more parts**

Given an S3 multipart upload in progress with registered parts,
When `handleListParts` builds the `ListPartsResult` XML,
Then the formatted timestamp is produced by `time.AppendFormat` into a
stack-allocated buffer and written with `buf.Write`,
And the emitted `LastModified` value uses the `time.RFC3339` layout.

### BL-A2: Byte-identical XML output

**Requirement:** The XML bytes emitted by `handleListParts` MUST be identical
before and after the change.

**Scenario: Existing ListParts output is unchanged**

Given a multipart upload with parts,
When the ListParts response XML is compared before and after the optimization,
Then the `LastModified` element values are byte-for-byte identical,
And no protocol or schema change is introduced.

### BL-A3: No allocation regression (benchmark gate)

**Requirement:** The optimization MUST remove the per-response timestamp
allocation.

**Scenario: Benchmark measures allocation count**

Given the `BenchmarkListPartsTime_AppendFormat` microbenchmark,
When the benchmark runs with `-benchmem`,
Then the result reports `0 allocs/op` (vs `1 alloc/op` for the previous
`time.Format` implementation).

## Non-Goals

- No changes to other S3 handlers' panic-recovery or input validation.
- No behavioral surface change to the S3 protocol.