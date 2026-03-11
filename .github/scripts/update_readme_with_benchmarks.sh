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
    sum[$1] += $3;
    unit[$1] = $4;
    count[$1]++;
}
END {
    for (bench in sum) {
        print bench, sum[bench]/count[bench], unit[bench]
    }
}' new_bench_filtered.txt | sort)

# Generate markdown table from the new benchmarks
MARKDOWN_TABLE="
| Benchmark | Avg. Value/Op |
|-----------|---------------|
"
while IFS= read -r line; do
    name=$(echo "$line" | awk '{print $1}')
    avg_val=$(echo "$line" | awk '{printf "%.2f", $2}')
    unit=$(echo "$line" | awk '{print $3}')
    MARKDOWN_TABLE="$MARKDOWN_TABLE| $name | $avg_val $unit |\n"
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

### Performance Chart

\`\`\`mermaid
xychart-beta
    title "Latest Benchmark Performance (Time)"
    x-axis "Benchmark"
    y-axis "Avg. Time (ns/op)"
EOF

# Filter for time-based benchmarks for the chart
TIME_AVG_RESULTS=$(echo "$AVG_RESULTS" | grep "ns/op")

X_AXIS_LABELS=""
BAR_DATA=""

while IFS= read -r line; do
    # Shorten the name for the chart
    name=$(echo "$line" | awk '{print $1}' | sed -e 's/Benchmark//' -e 's/-[0-9]\+$//')
    avg_time=$(echo "$line" | awk '{printf "%.0f", $2}')
    X_AXIS_LABELS="\"$name\", $X_AXIS_LABELS"
    BAR_DATA="$avg_time, $BAR_DATA"
done <<< "$TIME_AVG_RESULTS"

# Remove trailing commas
if [ -n "$X_AXIS_LABELS" ]; then
    X_AXIS_LABELS=${X_AXIS_LABELS%, }
    BAR_DATA=${BAR_DATA%, }
fi

cat <<EOF >> "$CONTENT_FILE"
    x-axis [${X_AXIS_LABELS}]
    bar [${BAR_DATA}]
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
