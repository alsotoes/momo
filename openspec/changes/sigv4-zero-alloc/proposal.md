# SigV4 Zero-Allocation Parsing

## The Problem
SigV4 request verification happens on every authenticated request. The original implementation resulted in ~7 heap allocations per request (via slice creation, MIME header canonicalization, etc.), putting unnecessary pressure on the GC on a hot path.

## The Solution
Optimize `buildCanonicalHeaders` to eliminate heap allocations by replacing `strings.Split` with `strings.IndexByte`, using a stack buffer `var stackHeaders [16]string`, avoiding `req.Header.Get()` overhead, and slicing manually instead of using `strings.TrimSpace`. Add observable panic recovery and slice bounds safety (including a 4KB metadata limit check) according to steering rules.
