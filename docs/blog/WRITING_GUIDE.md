# Writing Educational Blog Posts — Momo Engineering Journal

## The Goal

Posts should be **teachable narratives**: a reader should come away understanding not just *what* we did, but *why* it matters, *how* to recognize when to apply the pattern, and *what trade-offs* to weigh. The journal is for us (history) AND for others (learning).

## Required Structure

Every post MUST include these sections (in order):

### 1. The Real Problem (Human Context)
- What pain motivated this? A production incident? A profiling surprise? A design dead-end?
- "We noticed..." / "During load testing..." / "A user reported..." / "We kept hitting..."
- Include the **human moment**: frustration, surprise, the "aha!" insight.

### 2. Why the Obvious Solutions Failed (Trade-offs)
- What did you try first? Why wasn't it enough?
- What alternatives did you consider and reject? (e.g., "We considered pooling but...")
- This is where the *teaching* lives — readers learn from your dead ends.

### 3. The Solution — With Code Walkthrough
- Show the change, but **annotate each decision**:
  ```go
  // Stack buffer: avoids heap allocation entirely
  // 32 bytes fits RFC3339Nano (20 chars) + timezone offset + padding
  var timeBuf [32]byte
  
  // AppendFormat writes directly to buffer, returns extended slice
  // No intermediate string = zero allocation
  // timeBuf[:0] creates empty slice backed by the array
  buf.Write(t.AppendFormat(timeBuf[:0], time.RFC3339Nano))
  ```
- Explain *why* this specific approach (not just what it does).

### 4. The Principle (Reusable Pattern)
- Extract the **generalizable lesson** in a callout box:
> **Pattern: Stack-Buffer Time Formatting**
> When formatting timestamps in hot paths, use `time.AppendFormat` with a stack-allocated `byte` array instead of `time.Format`. Eliminates heap allocation per call.
> 
> **Applies when**: high-frequency timestamp rendering (HTTP headers, XML/JSON fields, log lines)
> **Doesn't apply**: one-off formatting, human-readable output where allocation is negligible
>
> ```go
> // ✅ GOOD: Hot path — HTTP header, XML field, log line
> func writeLastModified(buf *bytes.Buffer, modTime int64) {
>     var timeBuf [32]byte
>     t := time.Unix(0, modTime).UTC()
>     buf.Write(t.AppendFormat(timeBuf[:0], time.RFC3339Nano))
> }
> 
> // ❌ AVOID: Low-frequency path — allocation negligible
> func formatForDisplay(modTime int64) string {
>     return time.Unix(0, modTime).Format("Jan 2, 2006")
> }
> ```

### 5. How We Verified (Measurement)
- Benchmarks, profiles, production metrics
- Show the numbers: "32 B/op → 0 B/op", "p99 latency -15%"
- Link to benchmark code or CI artifact

### 6. What Could Go Wrong (Failure Modes)
- Buffer too small? Timezone edge cases? Thread safety?
- How the code guards against these (or where it doesn't yet)

### 7. When NOT to Use This (Boundaries)
- Explicitly state the limits — prevents cargo-culting
- "Don't use this for request IDs, user-facing dates, or low-frequency paths"

### 8. Related & References
- Link to specs, PRs, issues, sibling posts (existing `related` field)
- External references (Go blog posts, papers, prior art)

---

## Tone Guidelines

| ✅ Do | ❌ Don't |
|------|---------|
| "We hit this wall..." | "The system was optimized..." |
| "The insight came when..." | "The solution is..." |
| "We rejected X because..." | "X is bad" |
| "This pattern applies when..." | "Always do this" |
| Show the dead ends | Only show the happy path |
| Admit uncertainty ("we're watching...") | Claim perfection |

---

## Front Matter Additions (Optional but Encouraged)

```yaml
# Add to front matter for richer categorization
difficulty: "intermediate"  # beginner | intermediate | advanced
pattern: "zero-alloc"       # or "deadline-amortization", "verify-on-read", etc.
teaches: 
  - "time.AppendFormat vs time.Format"
  - "stack buffer allocation pattern"
  - "hot path identification via pprof"
```

---

## Example Transformation

### Before (Current 047)
> "In our continuous pursuit of performance, we identified a small but frequent allocation... The solution was straightforward..."

### After (Target)
> **The Real Problem**: During a 100k req/s S3 load test, the GC trace showed `time.Format` allocating 32 bytes per `CopyObject` response. At peak, that's ~3 MB/s of garbage just for timestamps. The GC pause spiked to 12ms — unacceptable for our p99 target.
>
> **Why the Obvious Failed**: We first tried a `sync.Pool` of strings. But pool overhead (mutex, GC pressure from pooled objects) was worse than the allocation. We considered caching formatted strings but timestamps are unique per request.
>
> **The Solution**: `time.AppendFormat` with a stack buffer...
>
> **Principle**: Stack-Buffer Time Formatting...
>
> **When NOT to Use**: ...

---

## Checklist Before Publishing

- [ ] Opens with a concrete problem/pain point (not "we optimized")
- [ ] Shows what was tried and rejected (teaches trade-offs)
- [ ] Code has inline comments explaining *why*, not *what*
- [ ] Has a "Principle" callout box with applicability boundaries
- [ ] Shows measurements (benchmarks, profiles, prod metrics)
- [ ] Lists failure modes and how they're handled
- [ ] Explicitly states when NOT to use the pattern
- [ ] Links to source artifacts (spec, PR, issue, benchmark code)
- [ ] Tone is human ("we", "our", "I") not institutional