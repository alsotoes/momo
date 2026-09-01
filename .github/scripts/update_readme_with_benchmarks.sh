#!/bin/bash
set -e
set -x # Enable debugging for CI logs

# This script updates the README.md with benchmark results.
# It expects two arguments: the path to the old benchmark results
# and the path to the new benchmark results.

OLD_BENCH=$1
NEW_BENCH=$2
README_FILE="docs/PERFORMANCE.md"
MARKER_START="<!-- BENCHMARK_RESULTS_START -->"
MARKER_END="<!-- BENCHMARK_RESULTS_END -->"

if [ ! -f "$OLD_BENCH" ] || [ ! -f "$NEW_BENCH" ]; then
    echo "Usage: $0 <old_bench.txt> <new_bench.txt>"
    exit 1
fi

# Filter benchmark results to only include benchmark lines
grep "^Benchmark" "$OLD_BENCH" > /tmp/old_bench_filtered.txt || true
grep "^Benchmark" "$NEW_BENCH" > /tmp/new_bench_filtered.txt || true

# Generate comparison table with benchstat
COMPARISON=$(benchstat /tmp/old_bench_filtered.txt /tmp/new_bench_filtered.txt || true)

# Average the results for the table and chart
AVG_RESULTS=$(awk '
{
    sum_ns[$1] += $3;
    sum_B[$1] += $5;
    sum_allocs[$1] += $7;
    count[$1]++;
}
END {
    for (bench in sum_ns) {
        print bench, sum_ns[bench]/count[bench], sum_B[bench]/count[bench], sum_allocs[bench]/count[bench]
    }
}' /tmp/new_bench_filtered.txt | sort)

# Generate markdown table from the new benchmarks
MARKDOWN_TABLE="
| Benchmark | Avg. Time/Op | Avg. Bytes/Op | Avg. Allocs/Op |
|-----------|--------------|---------------|----------------|
"
while IFS= read -r line; do
    name=$(echo "$line" | awk '{print $1}')
    avg_ns=$(echo "$line" | awk '{printf "%.2f", $2}')
    avg_B=$(echo "$line" | awk '{printf "%.2f", $3}')
    avg_allocs=$(echo "$line" | awk '{printf "%.2f", $4}')
    MARKDOWN_TABLE="${MARKDOWN_TABLE}| $name | $avg_ns ns/op | $avg_B B/op | $avg_allocs allocs/op |
"
done <<< "$AVG_RESULTS"

HISTORY_FILE=".github/data/benchmark_history.csv"
COMMIT_SHA=$3

# Save the latest averaged results to the history file
while IFS= read -r line; do
    name=$(echo "$line" | awk '{print $1}')
    avg_ns=$(echo "$line" | awk '{printf "%.2f", $2}')
    avg_B=$(echo "$line" | awk '{printf "%.2f", $3}')
    avg_allocs=$(echo "$line" | awk '{printf "%.2f", $4}')
    echo "$COMMIT_SHA,$name,$avg_ns,$avg_B,$avg_allocs" >> "$HISTORY_FILE"
done <<< "$AVG_RESULTS"

# Get the list of unique benchmark names (stripping core-count suffixes like -4, -8)
BENCHMARK_NAMES=$(awk -F, 'NR>1 {print $2}' "$HISTORY_FILE" | sed -E 's/-[0-9]+$//' | sort -u)

# Function to get description for a benchmark
get_desc() {
    case "$1" in
        "CheckMetricsAndSwap") echo "Evaluation of system metrics (CPU/Mem) and mode switching logic" ;;
        "CrushOptimized") echo "Performance-tuned CRUSH-lite placement algorithm using bitwise shifts and integer math (Rule 19)" ;;
        "CrushOriginal") echo "Original Sage Weil's CRUSH placement algorithm using reflection and float math" ;;
        "IndexDirectTracking") echo "Accessing current replication mode via direct slice index (O(1))" ;;
        "IndexSearch") echo "Searching for current replication mode in the order slice using \`slices.Index\`" ;;
        "LoadGlobalConfig") echo "Parsing and loading the \`[global]\` section from the INI configuration" ;;
        "PadString") echo "Padding strings with null characters to a fixed protocol length" ;;
        "ParseReplicationOrder"*) echo "Parsing the CSV-formatted replication order string into an integer slice" ;;
        *) echo "No description available" ;;
    esac
}

# Prepare the content to be injected into the README
# Use a temporary file to avoid issues with special characters in variables.
CONTENT_FILE=$(mktemp)
cat <<EOF > "$CONTENT_FILE"
## Performance

This section is automatically updated by the pre-commit hook (via .github/scripts/update_readme_with_benchmarks.sh).

### Comparison with previous commit

Each table compares the previous commit (left) to the current commit (right).
**Lower is better** in all three tables. \`+X%\` = regression, \`-X%\` = improvement,
\`~\` = no statistically significant change (p >= 0.05).

- **sec/op**: Time per operation (nanoseconds). Measures raw speed.
- **B/op**: Bytes allocated on the heap per operation. Measures memory usage.
- **allocs/op**: Number of heap allocations per operation. Measures GC pressure.

\`\`\`
$COMPARISON
\`\`\`

### Latest Benchmark Results

Single-snapshot of the current commit's averages. **Lower is better** across all
three columns: time (speed), bytes (memory), allocs (GC pressure).

$MARKDOWN_TABLE

### Performance History

**Legend**

| Color | Benchmark | Description |
|---|---|---|
| 🟢 | CheckMetricsAndSwap | $(get_desc "CheckMetricsAndSwap") |
| 🟤 | CrushOriginal | $(get_desc "CrushOriginal") |
| ⚪ | CrushOptimized | $(get_desc "CrushOptimized") |
| 🔵 | IndexDirectTracking | $(get_desc "IndexDirectTracking") |
| 🔴 | IndexSearch | $(get_desc "IndexSearch") |
| 🟠 | LoadGlobalConfig | $(get_desc "LoadGlobalConfig") |
| 🟣 | PadString | $(get_desc "PadString") |
| 🟡 | ParseReplicationOrder | $(get_desc "ParseReplicationOrder") |

#### Time per Operation (ns/op)

X-axis: commit SHA (left = older, right = newer). Y-axis: avg time in ns/op.
**Lower lines = faster code.** Downward trends are improvements; upward spikes
are regressions to investigate.

\`\`\`mermaid
xychart-beta
    title "Performance Trend (Avg. Time, Last 10 Commits)"
    x-axis "Commit"
    y-axis "Avg. Time (ns/op)"
EOF

# Get the last 10 unique commit SHAs from the history
LAST_10_COMMITS=$(tail -n 200 "$HISTORY_FILE" | awk -F, '{print $1}' | uniq | tail -n 10 | sed -e 's/^\(.......\).*/\1/g' | tr '\n' ',' | sed 's/,$//')

cat <<EOF >> "$CONTENT_FILE"
    x-axis [${LAST_10_COMMITS}]
EOF

for bench_name in $BENCHMARK_NAMES; do
    # Get the data for this benchmark for the last 10 commits (matching exact column with or without core-count suffix)
    bench_data=$(grep -E ",${bench_name}(-[0-9]+)?," "$HISTORY_FILE" | tail -n 10 | awk -F, '{printf "%.0f,", $3}' | sed 's/,$//' || true)
    short_name=$(echo "$bench_name" | sed -e 's/Benchmark//')
    
    # Pad with leading zeros if we have fewer than 10 commits (e.g. for recently added benchmarks)
    count=$(echo "$bench_data" | tr -cd ',' | wc -c)
    if [ -n "$bench_data" ]; then
        num_points=$((count + 1))
    else
        num_points=0
    fi
    while [ $num_points -lt 10 ]; do
        bench_data="0,$bench_data"
        num_points=$((num_points + 1))
    done

    echo "    line \"$short_name\" [${bench_data}]" >> "$CONTENT_FILE"
done

cat <<EOF >> "$CONTENT_FILE"
\`\`\`

#### Memory per Operation (B/op)

X-axis: commit SHA. Y-axis: avg bytes allocated per op.
**Lower lines = less memory.** Drops indicate allocation reductions.

\`\`\`mermaid
xychart-beta
    title "Memory Trend (Avg. Bytes/Op, Last 10 Commits)"
    x-axis "Commit"
    y-axis "Avg. Bytes/Op"
    x-axis [${LAST_10_COMMITS}]
EOF

for bench_name in $BENCHMARK_NAMES; do
    # Get the data for this benchmark for the last 10 commits (matching exact column with or without core-count suffix)
    bench_data=$(grep -E ",${bench_name}(-[0-9]+)?," "$HISTORY_FILE" | tail -n 10 | awk -F, '{printf "%.0f,", $4}' | sed 's/,$//' || true)
    short_name=$(echo "$bench_name" | sed -e 's/Benchmark//')
    
    # Pad with leading zeros if we have fewer than 10 commits (e.g. for recently added benchmarks)
    count=$(echo "$bench_data" | tr -cd ',' | wc -c)
    if [ -n "$bench_data" ]; then
        num_points=$((count + 1))
    else
        num_points=0
    fi
    while [ $num_points -lt 10 ]; do
        bench_data="0,$bench_data"
        num_points=$((num_points + 1))
    done

    echo "    line \"$short_name\" [${bench_data}]" >> "$CONTENT_FILE"
done

cat <<EOF >> "$CONTENT_FILE"
\`\`\`

#### Allocations per Operation (allocs/op)

X-axis: commit SHA. Y-axis: avg heap allocations per op.
**Lower lines = fewer allocations.** Fewer allocs = less GC pressure.

\`\`\`mermaid
xychart-beta
    title "Allocation Trend (Avg. Allocs/Op, Last 10 Commits)"
    x-axis "Commit"
    y-axis "Avg. Allocs/Op"
    x-axis [${LAST_10_COMMITS}]
EOF

for bench_name in $BENCHMARK_NAMES; do
    # Get the data for this benchmark for the last 10 commits (matching exact column with or without core-count suffix)
    bench_data=$(grep -E ",${bench_name}(-[0-9]+)?," "$HISTORY_FILE" | tail -n 10 | awk -F, '{printf "%.0f,", $5}' | sed 's/,$//' || true)
    short_name=$(echo "$bench_name" | sed -e 's/Benchmark//')
    
    # Pad with leading zeros if we have fewer than 10 commits (e.g. for recently added benchmarks)
    count=$(echo "$bench_data" | tr -cd ',' | wc -c)
    if [ -n "$bench_data" ]; then
        num_points=$((count + 1))
    else
        num_points=0
    fi
    while [ $num_points -lt 10 ]; do
        bench_data="0,$bench_data"
        num_points=$((num_points + 1))
    done

    echo "    line \"$short_name\" [${bench_data}]" >> "$CONTENT_FILE"
done

cat <<EOF >> "$CONTENT_FILE"
\`\`\`
EOF

# Update the README
TMP_README=$(mktemp)

# Read the README line by line
in_bench_section=false
while IFS= read -r line; do
    if [[ "$line" == "$MARKER_START" ]]; then
        echo "$MARKER_START" >> "$TMP_README"
        cat "$CONTENT_FILE" >> "$TMP_README"
        in_bench_section=true
    elif [[ "$line" == "$MARKER_END" ]]; then
        in_bench_section=false
        echo "$MARKER_END" >> "$TMP_README"
    elif ! $in_bench_section; then
        echo "$line" >> "$TMP_README"
    fi
done < "$README_FILE"

# Clean up the temporary content file
rm "$CONTENT_FILE"

# Move the temporary file to the original README
mv "$TMP_README" "$README_FILE"

echo "PERFORMANCE.md updated with benchmark results."
