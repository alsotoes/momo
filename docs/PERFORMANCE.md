<!-- BENCHMARK_RESULTS_START -->
## Performance

This section is automatically updated by our GitHub Actions workflow.

### Comparison with previous commit

```
                      │ /tmp/old_bench_filtered.txt │      /tmp/new_bench_filtered.txt      │
                      │           sec/op            │    sec/op      vs base                │
CrushOriginal-8                        363.2n ± ∞ ¹    390.3n ± ∞ ¹       ~ (p=1.000 n=1) ²
CrushOptimized-8                       262.6n ± ∞ ¹    255.2n ± ∞ ¹       ~ (p=1.000 n=1) ²
LoadGlobalConfig-8                     619.3n ± ∞ ¹    612.7n ± ∞ ¹       ~ (p=1.000 n=1) ²
PadString-8                            1.700n ± ∞ ¹    1.747n ± ∞ ¹       ~ (p=1.000 n=1) ²
CheckMetricsAndSwap-8                  6.028n ± ∞ ¹    6.293n ± ∞ ¹       ~ (p=1.000 n=1) ²
IndexSearch-8                          1.347n ± ∞ ¹    1.325n ± ∞ ¹       ~ (p=1.000 n=1) ²
IndexDirectTracking-8                 0.2900n ± ∞ ¹   0.3114n ± ∞ ¹       ~ (p=1.000 n=1) ²
geomean                                15.71n          16.07n        +2.28%
¹ need >= 6 samples for confidence interval at level 0.95
² need >= 4 samples to detect a difference at alpha level 0.05

                      │ /tmp/old_bench_filtered.txt │     /tmp/new_bench_filtered.txt     │
                      │            B/op             │    B/op      vs base                │
CrushOriginal-8                         164.0 ± ∞ ¹   164.0 ± ∞ ¹       ~ (p=1.000 n=1) ²
CrushOptimized-8                        0.000 ± ∞ ¹   0.000 ± ∞ ¹       ~ (p=1.000 n=1) ²
LoadGlobalConfig-8                      160.0 ± ∞ ¹   160.0 ± ∞ ¹       ~ (p=1.000 n=1) ²
PadString-8                             0.000 ± ∞ ¹   0.000 ± ∞ ¹       ~ (p=1.000 n=1) ²
CheckMetricsAndSwap-8                   0.000 ± ∞ ¹   0.000 ± ∞ ¹       ~ (p=1.000 n=1) ²
IndexSearch-8                           0.000 ± ∞ ¹   0.000 ± ∞ ¹       ~ (p=1.000 n=1) ²
IndexDirectTracking-8                   0.000 ± ∞ ¹   0.000 ± ∞ ¹       ~ (p=1.000 n=1) ²
geomean                                           ³                +0.00%               ³
¹ need >= 6 samples for confidence interval at level 0.95
² all samples are equal
³ summaries must be >0 to compute geomean

                      │ /tmp/old_bench_filtered.txt │     /tmp/new_bench_filtered.txt     │
                      │          allocs/op          │  allocs/op   vs base                │
CrushOriginal-8                         3.000 ± ∞ ¹   3.000 ± ∞ ¹       ~ (p=1.000 n=1) ²
CrushOptimized-8                        0.000 ± ∞ ¹   0.000 ± ∞ ¹       ~ (p=1.000 n=1) ²
LoadGlobalConfig-8                      1.000 ± ∞ ¹   1.000 ± ∞ ¹       ~ (p=1.000 n=1) ²
PadString-8                             0.000 ± ∞ ¹   0.000 ± ∞ ¹       ~ (p=1.000 n=1) ²
CheckMetricsAndSwap-8                   0.000 ± ∞ ¹   0.000 ± ∞ ¹       ~ (p=1.000 n=1) ²
IndexSearch-8                           0.000 ± ∞ ¹   0.000 ± ∞ ¹       ~ (p=1.000 n=1) ²
IndexDirectTracking-8                   0.000 ± ∞ ¹   0.000 ± ∞ ¹       ~ (p=1.000 n=1) ²
geomean                                           ³                +0.00%               ³
¹ need >= 6 samples for confidence interval at level 0.95
² all samples are equal
³ summaries must be >0 to compute geomean
```

### Latest Benchmark Results


| Benchmark | Avg. Time/Op | Avg. Bytes/Op | Avg. Allocs/Op |
|-----------|--------------|---------------|----------------|
| BenchmarkCheckMetricsAndSwap-8 | 6.29 ns/op | 0.00 B/op | 0.00 allocs/op |
| BenchmarkCrushOptimized-8 | 255.20 ns/op | 0.00 B/op | 0.00 allocs/op |
| BenchmarkCrushOriginal-8 | 390.30 ns/op | 164.00 B/op | 3.00 allocs/op |
| BenchmarkIndexDirectTracking-8 | 0.31 ns/op | 0.00 B/op | 0.00 allocs/op |
| BenchmarkIndexSearch-8 | 1.32 ns/op | 0.00 B/op | 0.00 allocs/op |
| BenchmarkLoadGlobalConfig-8 | 612.70 ns/op | 160.00 B/op | 1.00 allocs/op |
| BenchmarkPadString-8 | 1.75 ns/op | 0.00 B/op | 0.00 allocs/op |


### Performance History

**Legend**

| Color | Benchmark | Description |
|---|---|---|
| 🟢 | CheckMetricsAndSwap | Evaluation of system metrics (CPU/Mem) and mode switching logic |
| 🔵 | IndexDirectTracking | Accessing current replication mode via direct slice index (O(1)) |
| 🔴 | IndexSearch | Searching for current replication mode in the order slice using `slices.Index` |
| 🟠 | LoadGlobalConfig | Parsing and loading the `[global]` section from the INI configuration |
| 🟣 | PadString | Padding strings with null characters to a fixed protocol length |
| 🟡 | ParseReplicationOrder | Parsing the CSV-formatted replication order string into an integer slice |

```mermaid
xychart-beta
    title "Performance Trend (Avg. Time, Last 10 Commits)"
    x-axis "Commit"
    y-axis "Avg. Time (ns/op)"
    x-axis [17fa,0ba6,0b1a,83df,f015,9a0d,ca06,d431,f59b,loca]
    line "CheckMetricsAndSwap" [7,7,6,7,6,7,7,7,5,5]
    line "CheckMetricsAndSwap" [6,6,6,6,6]
    line "CrushOptimized" [256,258,263,255]
    line "CrushOriginal" [363,381,363,390]
    line "IndexDirectTracking" [0,0,0,0,0,0,0,0,0,0]
    line "IndexDirectTracking" [0,0,0,0,0]
    line "IndexSearch" [4,4,4,3,4,3,3,1,1,1]
    line "IndexSearch" [1,1,1,1,1]
    line "LoadGlobalConfig" [6,20,21,22,16,21,1,20,22,1]
    line "LoadGlobalConfig" [23,614,617,619,613]
    line "PadString" [1,1,1,1,1,1,1,1,1,1]
    line "PadString" [2,2,2,2,2]
    line "ParseReplicationOrder_NoPrealloc" [350,349,357,354,345,225,229,165,232,234]
    line "ParseReplicationOrder_Prealloc" [229,231,237,234,229,108,107,80,110,109]
```

```mermaid
xychart-beta
    title "Memory Trend (Avg. Bytes/Op, Last 10 Commits)"
    x-axis "Commit"
    y-axis "Avg. Bytes/Op"
    x-axis [17fa,0ba6,0b1a,83df,f015,9a0d,ca06,d431,f59b,loca]
    line "CheckMetricsAndSwap" [0,0,0,0,0,0,0,0,0,0]
    line "CheckMetricsAndSwap" [0,0,0,0,0]
    line "CrushOptimized" [0,0,0,0]
    line "CrushOriginal" [164,164,164,164]
    line "IndexDirectTracking" [0,0,0,0,0,0,0,0,0,0]
    line "IndexDirectTracking" [0,0,0,0,0]
    line "IndexSearch" [0,0,0,0,0,0,0,0,0,0]
    line "IndexSearch" [0,0,0,0,0]
    line "LoadGlobalConfig" [0,0,0,0,0,0,0,0,0,0]
    line "LoadGlobalConfig" [0,160,160,160,160]
    line "PadString" [0,0,0,0,0,0,0,0,0,0]
    line "PadString" [0,0,0,0,0]
    line "ParseReplicationOrder_NoPrealloc" [408,408,408,408,408,248,248,248,248,248]
    line "ParseReplicationOrder_Prealloc" [240,240,240,240,240,80,80,80,80,80]
```

```mermaid
xychart-beta
    title "Allocation Trend (Avg. Allocs/Op, Last 10 Commits)"
    x-axis "Commit"
    y-axis "Avg. Allocs/Op"
    x-axis [17fa,0ba6,0b1a,83df,f015,9a0d,ca06,d431,f59b,loca]
    line "CheckMetricsAndSwap" [0,0,0,0,0,0,0,0,0,0]
    line "CheckMetricsAndSwap" [0,0,0,0,0]
    line "CrushOptimized" [0,0,0,0]
    line "CrushOriginal" [3,3,3,3]
    line "IndexDirectTracking" [0,0,0,0,0,0,0,0,0,0]
    line "IndexDirectTracking" [0,0,0,0,0]
    line "IndexSearch" [0,0,0,0,0,0,0,0,0,0]
    line "IndexSearch" [0,0,0,0,0]
    line "LoadGlobalConfig" [0,0,0,0,0,0,0,0,0,0]
    line "LoadGlobalConfig" [0,1,1,1,1]
    line "PadString" [0,0,0,0,0,0,0,0,0,0]
    line "PadString" [0,0,0,0,0]
    line "ParseReplicationOrder_NoPrealloc" [6,6,6,6,6,5,5,5,5,5]
    line "ParseReplicationOrder_Prealloc" [2,2,2,2,2,1,1,1,1,1]
```
<!-- BENCHMARK_RESULTS_END -->
