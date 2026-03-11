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

# Generate MermaidJS chart and markdown table from the new benchmarks
# Using awk to parse the benchmark output
PARSED_RESULTS=$(awk '{print $1, $3, $4}' new_bench_filtered.txt)

MARKDOWN_TABLE="
| Benchmark | Time/Op |
|-----------|---------|
"
while IFS= read -r line; do
    name=$(echo "$line" | awk '{print $1}')
    time=$(echo "$line" | awk '{print $2}')
    unit=$(echo "$line" | awk '{print $3}')
    MARKDOWN_TABLE="$MARKDOWN_TABLE| $name | $time $unit |
"
done <<< "$PARSED_RESULTS"

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
gantt
    title Latest Benchmark Results
    dateFormat  X
    axisFormat  %s
EOF

while IFS= read -r line; do
    name=$(echo "$line" | awk '{print $1}')
    time=$(echo "$line" | awk '{print $2}')
    # Mermaid gantt chart wants integer times. We will strip the decimals.
    time_int=$(echo "$time" | awk -F. '{print $1}')
    echo "    $name : $time_int" >> "$CONTENT_FILE"
done <<< "$PARSED_RESULTS"

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
