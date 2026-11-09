# Zero Allocation S3 Listing
Resolves #1285

## Specification
The list-versions formatting layer (`FormatListVersionsXML`) must be refactored to eliminate all allocations:
1. `xml.MarshalIndent` must be removed and replaced with a zero-allocation XML generation method leveraging a single `bytes.Buffer`.
2. XML formatting must use stack-allocated fixed-size buffers, utilizing functions like `time.AppendFormat` and `strconv.AppendInt`.
3. The function must include a `defer recover()` panic handler assigning `syscall.EIO` and safely logging to standard output/error to ensure any memory corruption, pointer issues, or index out-of-bounds in manual formatting logic does not crash the gateway daemon.
