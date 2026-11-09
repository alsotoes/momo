# Zero Allocation S3 Listing
Resolves #1285

## Context
Formatting S3 list responses dynamically with `xml.MarshalIndent` and `time.Format` inside large array serialization loops creates significant garbage collection pressure due to heavy heap allocation overhead per element. This degrades performance on the S3 metadata retrieval hot path.

## Proposal
The `FormatListVersionsXML` response builder will be refactored to use manual XML generation. We will eliminate dynamic heap allocations by writing directly to a `bytes.Buffer` and locally generating strings using `time.AppendFormat` and `strconv.AppendInt` against fixed-size stack arrays (`[32]byte`), enabling zero-allocation string appending.
