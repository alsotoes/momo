#!/bin/bash
set -e

# This script updates the README.md with benchmark results.
# It expects two arguments: the path to the old benchmark results
# and the path to the new benchmark results.

OLD_BENCH=$1
NEW_BENCH=$2
README_FILE="doc/README.md"
MARKER_START="<!-- BENCHMARK_RESULTS_START -->"
MARKER_END="<!-- BENCHMARK_RESULTS_END -->"

if [ ! -f "$OLD_BENCH" ] || [ ! -f "$NEW_BENCH" ]; then
    echo "Usage: $0 <old_bench.txt> <new_bench.txt>"
    exit 1
fi

# Filter benchmark results to only include benchmark lines
grep "^Benchmark" "$OLD_BENCH" > old_bench_filtered.txt
grep "^Benchmark" "$NEW_BENCH" > new_bench_filtered.txt

# Generate comparison table with benchstat
COMPARISON=$(benchstat old_bench_filtered.txt new_bench_filtered.txt)

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
}' new_bench_filtered.txt | sort)

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
    MARKDOWN_TABLE="$MARKDOWN_TABLE| $name | $avg_ns ns/op | $avg_B B/op | $avg_allocs allocs/op |\n"
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

# Prepare the content to be injected into the README
# Use a temporary file to avoid issues with special characters in variables.
CONTENT_FILE=$(mktemp)
cat <<EOF > "$CONTENT_FILE"
## Performance

This section is automatically updated by our GitHub Actions workflow.

### Comparison with previous commit

\`\`\`
$COMPARISON
\`\`\`

### Latest Benchmark Results

$MARKDOWN_TABLE

### Performance History

\`\`\`mermaid
xychart-beta
    title "Performance Trend (Avg. Time, Last 10 Commits)"
    x-axis "Commit"
    y-axis "Avg. Time (ns/op)"
EOF

# Get the last 10 unique commit SHAs from the history
LAST_10_COMMITS=$(tail -n 40 "$HISTORY_FILE" | awk -F, '{print $1}' | uniq | tail -n 10 | sed -e 's/^\(....\).*/\1/g' | tr '\n' ',' | sed 's/,$//')

cat <<EOF >> "$CONTENT_FILE"
    x-axis [${LAST_10_COMMITS}]
EOF

# Get the list of unique benchmark names
BENCHMARK_NAMES=$(awk -F, 'NR>1 {print $2}' "$HISTORY_FILE" | sort -u)

for bench_name in $BENCHMARK_NAMES; do
    # Get the data for this benchmark for the last 10 commits
    bench_data=$(grep "$bench_name" "$HISTORY_FILE" | tail -n 10 | awk -F, '{printf "%.0f,", $3}' | sed 's/,$//')
    short_name=$(echo "$bench_name" | sed -e 's/Benchmark//' -e 's/-[0-9]\+$//')
    echo "    line \"$short_name\" [${bench_data}]" >> "$CONTENT_FILE"
done

cat <<EOF >> "$CONTENT_FILE"
\`\`\`

\`\`\`mermaid
xychart-beta
    title "Memory Trend (Avg. Bytes/Op, Last 10 Commits)"
    x-axis "Commit"
    y-axis "Avg. Bytes/Op"
    x-axis [${LAST_10_COMMITS}]
EOF

for bench_name in $BENCHMARK_NAMES; do
    # Get the data for this benchmark for the last 10 commits
    bench_data=$(grep "$bench_name" "$HISTORY_FILE" | tail -n 10 | awk -F, '{printf "%.0f,", $4}' | sed 's/,$//')
    short_name=$(echo "$bench_name" | sed -e 's/Benchmark//' -e 's/-[0-9]\+$//')
    echo "    line \"$short_name\" [${bench_data}]" >> "$CONTENT_FILE"
done

cat <<EOF >> "$CONTENT_FILE"
\`\`\`

\`\`\`mermaid
xychart-beta
    title "Allocation Trend (Avg. Allocs/Op, Last 10 Commits)"
    x-axis "Commit"
    y-axis "Avg. Allocs/Op"
    x-axis [${LAST_10_COMMITS}]
EOF

for bench_name in $BENCHMARK_NAMES; do
    # Get the data for this benchmark for the last 10 commits
    bench_data=$(grep "$bench_name" "$HISTORY_FILE" | tail -n 10 | awk -F, '{printf "%.0f,", $5}' | sed 's/,$//')
    short_name=$(echo "$bench_name" | sed -e 's/Benchmark//' -e 's/-[0-9]\+$//')
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

echo "README.md updated with benchmark results."
