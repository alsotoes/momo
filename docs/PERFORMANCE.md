<!-- BENCHMARK_RESULTS_START -->
## Performance

This section is automatically updated by our GitHub Actions workflow.

### Comparison with previous commit

```
                      │ /tmp/old_bench_filtered.txt │      /tmp/new_bench_filtered.txt       │
                      │           sec/op            │    sec/op      vs base                 │
CrushOriginal-8                        740.5n ± ∞ ¹    477.6n ± ∞ ¹        ~ (p=1.000 n=1) ²
CrushOptimized-8                       458.5n ± ∞ ¹    369.5n ± ∞ ¹        ~ (p=1.000 n=1) ²
LoadGlobalConfig-8                     1.870µ ± ∞ ¹    3.519µ ± ∞ ¹        ~ (p=1.000 n=1) ²
PadString-8                            2.841n ± ∞ ¹    2.078n ± ∞ ¹        ~ (p=1.000 n=1) ²
CheckMetricsAndSwap-8                 12.990n ± ∞ ¹    9.670n ± ∞ ¹        ~ (p=1.000 n=1) ²
IndexSearch-8                          2.242n ± ∞ ¹    1.847n ± ∞ ¹        ~ (p=1.000 n=1) ²
IndexDirectTracking-8                 0.6588n ± ∞ ¹   0.3899n ± ∞ ¹        ~ (p=1.000 n=1) ²
geomean                                32.03n          26.42n        -17.52%
¹ need >= 6 samples for confidence interval at level 0.95
² need >= 4 samples to detect a difference at alpha level 0.05

                      │ /tmp/old_bench_filtered.txt │     /tmp/new_bench_filtered.txt      │
                      │            B/op             │    B/op      vs base                 │
CrushOriginal-8                         164.0 ± ∞ ¹   164.0 ± ∞ ¹        ~ (p=1.000 n=1) ²
CrushOptimized-8                        0.000 ± ∞ ¹   0.000 ± ∞ ¹        ~ (p=1.000 n=1) ²
LoadGlobalConfig-8                      208.0 ± ∞ ¹   672.0 ± ∞ ¹        ~ (p=1.000 n=1) ³
PadString-8                             0.000 ± ∞ ¹   0.000 ± ∞ ¹        ~ (p=1.000 n=1) ²
CheckMetricsAndSwap-8                   0.000 ± ∞ ¹   0.000 ± ∞ ¹        ~ (p=1.000 n=1) ²
IndexSearch-8                           0.000 ± ∞ ¹   0.000 ± ∞ ¹        ~ (p=1.000 n=1) ²
IndexDirectTracking-8                   0.000 ± ∞ ¹   0.000 ± ∞ ¹        ~ (p=1.000 n=1) ²
geomean                                           ⁴                +18.24%               ⁴
¹ need >= 6 samples for confidence interval at level 0.95
² all samples are equal
³ need >= 4 samples to detect a difference at alpha level 0.05
⁴ summaries must be >0 to compute geomean

                      │ /tmp/old_bench_filtered.txt │      /tmp/new_bench_filtered.txt      │
                      │          allocs/op          │  allocs/op    vs base                 │
CrushOriginal-8                         3.000 ± ∞ ¹    3.000 ± ∞ ¹        ~ (p=1.000 n=1) ²
CrushOptimized-8                        0.000 ± ∞ ¹    0.000 ± ∞ ¹        ~ (p=1.000 n=1) ²
LoadGlobalConfig-8                      3.000 ± ∞ ¹   17.000 ± ∞ ¹        ~ (p=1.000 n=1) ³
PadString-8                             0.000 ± ∞ ¹    0.000 ± ∞ ¹        ~ (p=1.000 n=1) ²
CheckMetricsAndSwap-8                   0.000 ± ∞ ¹    0.000 ± ∞ ¹        ~ (p=1.000 n=1) ²
IndexSearch-8                           0.000 ± ∞ ¹    0.000 ± ∞ ¹        ~ (p=1.000 n=1) ²
IndexDirectTracking-8                   0.000 ± ∞ ¹    0.000 ± ∞ ¹        ~ (p=1.000 n=1) ²
geomean                                           ⁴                 +28.12%               ⁴
¹ need >= 6 samples for confidence interval at level 0.95
² all samples are equal
³ need >= 4 samples to detect a difference at alpha level 0.05
⁴ summaries must be >0 to compute geomean
```

### Latest Benchmark Results


| Benchmark | Avg. Time/Op | Avg. Bytes/Op | Avg. Allocs/Op |
|-----------|--------------|---------------|----------------|
| BenchmarkCheckMetricsAndSwap-8 | 9.67 ns/op | 0.00 B/op | 0.00 allocs/op |
| BenchmarkCrushOptimized-8 | 369.50 ns/op | 0.00 B/op | 0.00 allocs/op |
| BenchmarkCrushOriginal-8 | 477.60 ns/op | 164.00 B/op | 3.00 allocs/op |
| BenchmarkIndexDirectTracking-8 | 0.39 ns/op | 0.00 B/op | 0.00 allocs/op |
| BenchmarkIndexSearch-8 | 1.85 ns/op | 0.00 B/op | 0.00 allocs/op |
| BenchmarkLoadGlobalConfig-8 | 3519.00 ns/op | 672.00 B/op | 17.00 allocs/op |
| BenchmarkPadString-8 | 2.08 ns/op | 0.00 B/op | 0.00 allocs/op |


### Performance History

**Legend**

| Color | Benchmark | Description |
|---|---|---|
| 🟢 | CheckMetricsAndSwap | Evaluation of system metrics (CPU/Mem) and mode switching logic |
| 🟤 | CrushOriginal | Original Sage Weil's CRUSH placement algorithm using reflection and float math |
| ⚪ | CrushOptimized | Performance-tuned CRUSH-lite placement algorithm using bitwise shifts and integer math (Rule 19) |
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
    x-axis [33b4539,fe922ae,e2febb4,1acac21,58348d3,51b845f,eec8e9d,cf91a6b,ade4126,844727b]
    line "CheckMetricsAndSwap" [8,8,8,15,14,11,11,15,13,10]
    line "CrushOptimized" [314,290,341,544,466,437,449,463,458,370]
    line "CrushOriginal" [385,392,484,833,650,568,624,726,740,478]
    line "IndexDirectTracking" [0,0,1,1,1,0,0,1,1,0]
    line "IndexSearch" [2,2,2,3,2,2,2,2,2,2]
    line "LoadGlobalConfig" [753,729,912,1568,1350,1163,1661,2063,1870,3519]
    line "PadString" [2,2,2,4,3,2,3,3,3,2]
    line "ParseReplicationOrder_NoPrealloc" [350,349,357,354,345,225,229,165,232,234]
    line "ParseReplicationOrder_Prealloc" [229,231,237,234,229,108,107,80,110,109]
```

```mermaid
xychart-beta
    title "Memory Trend (Avg. Bytes/Op, Last 10 Commits)"
    x-axis "Commit"
    y-axis "Avg. Bytes/Op"
    x-axis [33b4539,fe922ae,e2febb4,1acac21,58348d3,51b845f,eec8e9d,cf91a6b,ade4126,844727b]
    line "CheckMetricsAndSwap" [0,0,0,0,0,0,0,0,0,0]
    line "CrushOptimized" [0,0,0,0,0,0,0,0,0,0]
    line "CrushOriginal" [164,164,164,164,164,164,164,164,164,164]
    line "IndexDirectTracking" [0,0,0,0,0,0,0,0,0,0]
    line "IndexSearch" [0,0,0,0,0,0,0,0,0,0]
    line "LoadGlobalConfig" [160,160,160,160,160,160,208,208,208,672]
    line "PadString" [0,0,0,0,0,0,0,0,0,0]
    line "ParseReplicationOrder_NoPrealloc" [408,408,408,408,408,248,248,248,248,248]
    line "ParseReplicationOrder_Prealloc" [240,240,240,240,240,80,80,80,80,80]
```

```mermaid
xychart-beta
    title "Allocation Trend (Avg. Allocs/Op, Last 10 Commits)"
    x-axis "Commit"
    y-axis "Avg. Allocs/Op"
    x-axis [33b4539,fe922ae,e2febb4,1acac21,58348d3,51b845f,eec8e9d,cf91a6b,ade4126,844727b]
    line "CheckMetricsAndSwap" [0,0,0,0,0,0,0,0,0,0]
    line "CrushOptimized" [0,0,0,0,0,0,0,0,0,0]
    line "CrushOriginal" [3,3,3,3,3,3,3,3,3,3]
    line "IndexDirectTracking" [0,0,0,0,0,0,0,0,0,0]
    line "IndexSearch" [0,0,0,0,0,0,0,0,0,0]
    line "LoadGlobalConfig" [1,1,1,1,1,1,3,3,3,17]
    line "PadString" [0,0,0,0,0,0,0,0,0,0]
    line "ParseReplicationOrder_NoPrealloc" [6,6,6,6,6,5,5,5,5,5]
    line "ParseReplicationOrder_Prealloc" [2,2,2,2,2,1,1,1,1,1]
```
<!-- BENCHMARK_RESULTS_END -->
