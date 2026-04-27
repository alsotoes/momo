## 2026-04-25 - Prevent Wrap-Around in Fast Integer Parsing
**Learning:** When writing custom integer parsing functions in Go to avoid allocations (like `parsePaddedIntFast` reading from `[]byte`), checking `res > (1<<63-1)/10` is insufficient for `int64` overflow protection. It misses wrap-arounds on the final digit.
**Action:** Always include a check for the final digit: `if res == (1<<63-1)/10 && int64(c-'0') > (1<<63-1)%10` to correctly return `strconv.ErrRange`.

## 2026-04-25 - strconv.ParseInt Optimization Insight
**Learning:** In modern Go, `strconv.ParseInt(string(b[:i]), 10, 64)` is compiler-optimized and does not allocate strings on the heap, so rewriting it purely to remove allocations is unnecessary. However, a custom inline loop still avoids the overhead of function calls and generalized base-10 parsing logic, proving ~2x faster.
**Action:** When pursuing byte-level integer parsing optimizations in performance-critical network paths, measure speed, not just allocations, as custom parsing can reduce CPU time significantly even if allocations are already zero.
