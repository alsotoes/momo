<!-- BENCHMARK_RESULTS_START -->
## Performance

This section is automatically updated by our GitHub Actions workflow.

### Comparison with previous commit

```
                      │ /tmp/old_bench_filtered.txt │      /tmp/new_bench_filtered.txt       │
                      │           sec/op            │    sec/op      vs base                 │
CrushOriginal-8                        471.4n ± ∞ ¹    729.8n ± ∞ ¹        ~ (p=1.000 n=1) ²
CrushOptimized-8                       344.4n ± ∞ ¹    424.1n ± ∞ ¹        ~ (p=1.000 n=1) ²
LoadGlobalConfig-8                     875.8n ± ∞ ¹   1066.0n ± ∞ ¹        ~ (p=1.000 n=1) ²
PadString-8                            2.382n ± ∞ ¹    2.291n ± ∞ ¹        ~ (p=1.000 n=1) ²
CheckMetricsAndSwap-8                  8.841n ± ∞ ¹    8.876n ± ∞ ¹        ~ (p=1.000 n=1) ²
IndexSearch-8                          2.081n ± ∞ ¹    1.913n ± ∞ ¹        ~ (p=1.000 n=1) ²
IndexDirectTracking-8                 0.4077n ± ∞ ¹   0.4361n ± ∞ ¹        ~ (p=1.000 n=1) ²
geomean                                22.06n          24.69n        +11.95%
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
| BenchmarkCheckMetricsAndSwap-8 | 8.88 ns/op | 0.00 B/op | 0.00 allocs/op |
| BenchmarkCrushOptimized-8 | 424.10 ns/op | 0.00 B/op | 0.00 allocs/op |
| BenchmarkCrushOriginal-8 | 729.80 ns/op | 164.00 B/op | 3.00 allocs/op |
| BenchmarkIndexDirectTracking-8 | 0.44 ns/op | 0.00 B/op | 0.00 allocs/op |
| BenchmarkIndexSearch-8 | 1.91 ns/op | 0.00 B/op | 0.00 allocs/op |
| BenchmarkLoadGlobalConfig-8 | 1066.00 ns/op | 160.00 B/op | 1.00 allocs/op |
| BenchmarkPadString-8 | 2.29 ns/op | 0.00 B/op | 0.00 allocs/op |


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
    x-axis [cca012e,6217a5a,51d89f3,b9a9671,24142be,06940c3,d729a65,aef7e26,0d8ab81,0f28cb7]
    line "CheckMetricsAndSwap" [10,10,9,10,11,10,11,9,9,9]
    line "CrushOptimized" [389,328,292,363,376,378,331,302,344,424]
    line "CrushOriginal" [544,434,439,438,522,445,479,434,471,730]
    line "IndexDirectTracking" [0,0,0,1,0,0,0,0,0,0]
    line "IndexSearch" [2,2,2,3,2,2,2,2,2,2]
    line "LoadGlobalConfig" [940,1065,831,831,1196,882,941,800,876,1066]
    line "PadString" [2,2,2,3,3,2,2,2,2,2]
    line "ParseReplicationOrder_NoPrealloc" [350,349,357,354,345,225,229,165,232,234]
    line "ParseReplicationOrder_Prealloc" [229,231,237,234,229,108,107,80,110,109]
```

```mermaid
xychart-beta
    title "Memory Trend (Avg. Bytes/Op, Last 10 Commits)"
    x-axis "Commit"
    y-axis "Avg. Bytes/Op"
    x-axis [cca012e,6217a5a,51d89f3,b9a9671,24142be,06940c3,d729a65,aef7e26,0d8ab81,0f28cb7]
    line "CheckMetricsAndSwap" [0,0,0,0,0,0,0,0,0,0]
    line "CrushOptimized" [0,0,0,0,0,0,0,0,0,0]
    line "CrushOriginal" [164,164,164,164,164,164,164,164,164,164]
    line "IndexDirectTracking" [0,0,0,0,0,0,0,0,0,0]
    line "IndexSearch" [0,0,0,0,0,0,0,0,0,0]
    line "LoadGlobalConfig" [160,160,160,160,160,160,160,160,160,160]
    line "PadString" [0,0,0,0,0,0,0,0,0,0]
    line "ParseReplicationOrder_NoPrealloc" [408,408,408,408,408,248,248,248,248,248]
    line "ParseReplicationOrder_Prealloc" [240,240,240,240,240,80,80,80,80,80]
```

```mermaid
xychart-beta
    title "Allocation Trend (Avg. Allocs/Op, Last 10 Commits)"
    x-axis "Commit"
    y-axis "Avg. Allocs/Op"
    x-axis [cca012e,6217a5a,51d89f3,b9a9671,24142be,06940c3,d729a65,aef7e26,0d8ab81,0f28cb7]
    line "CheckMetricsAndSwap" [0,0,0,0,0,0,0,0,0,0]
    line "CrushOptimized" [0,0,0,0,0,0,0,0,0,0]
    line "CrushOriginal" [3,3,3,3,3,3,3,3,3,3]
    line "IndexDirectTracking" [0,0,0,0,0,0,0,0,0,0]
    line "IndexSearch" [0,0,0,0,0,0,0,0,0,0]
    line "LoadGlobalConfig" [1,1,1,1,1,1,1,1,1,1]
    line "PadString" [0,0,0,0,0,0,0,0,0,0]
    line "ParseReplicationOrder_NoPrealloc" [6,6,6,6,6,5,5,5,5,5]
    line "ParseReplicationOrder_Prealloc" [2,2,2,2,2,1,1,1,1,1]
```
<!-- BENCHMARK_RESULTS_END -->
