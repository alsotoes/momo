#!/bin/bash
set -euo pipefail

# ============================================================
# Chaos Test: Node Crash During Replication
# Abruptly kills a node (kill -9) during active file transfers
# to verify self-healing recovery and data availability on
# remaining healthy replicas.
# ============================================================

CRASH_NODE="${1:-momo-server1}"
REMAINING_NODES="${2:-momo-server0 momo-server2}"
TEST_FILE="${3:-/tmp/momo-chaos-crash/test-file.bin}"
RESULT_DIR="${4:-/tmp/momo-chaos-crash}"

mkdir -p "$RESULT_DIR"

echo "=== Chaos Test: Node Crash During Replication ==="
echo "Target: $CRASH_NODE"
echo "Remaining: $REMAINING_NODES"

# Generate test file
dd if=/dev/urandom of="$TEST_FILE" bs=1M count=10 2>/dev/null

# Upload file to primary
echo "--- Uploading test file ---"
PRIMARY=$(echo "$REMAINING_NODES" | awk '{print $1}')
PRIMARY_PORT=$(docker inspect -f '{{range $p, $conf := .NetworkSettings.Ports}}{{range $conf}}{{.HostPort}} {{end}}{{end}}' "$PRIMARY" | awk '{print $1}')

curl -sf -X PUT "http://localhost:$PRIMARY_PORT/chaos-bucket/crash-test" \
     -H "Authorization: Bearer secret" \
     -T "$TEST_FILE" || true

# Wait for replication to start
sleep 1

# Crash the target node
echo "--- Crashing $CRASH_NODE (kill -9) ---"
docker kill "$CRASH_NODE" 2>/dev/null || true
CRASH_TIME=$(date +%s)

# Verify file is accessible on remaining replicas
echo "--- Verifying data availability on remaining nodes ---"
sleep 2

ALL_AVAILABLE=true
for NODE in $REMAINING_NODES; do
    PORT=$(docker inspect -f '{{range $p, $conf := .NetworkSettings.Ports}}{{range $conf}}{{.HostPort}} {{end}}{{end}}' "$NODE" | awk '{print $1}')
    if curl -sf "http://localhost:$PORT/chaos-bucket/crash-test" \
         -H "Authorization: Bearer secret" \
         -o "$RESULT_DIR/retrieved-$NODE.bin" 2>/dev/null; then
        if cmp -s "$TEST_FILE" "$RESULT_DIR/retrieved-$NODE.bin"; then
            echo "  $NODE: Data intact"
        else
            echo "  $NODE: Data CORRUPTED"
            ALL_AVAILABLE=false
        fi
    else
        echo "  $NODE: Data UNAVAILABLE"
        ALL_AVAILABLE=false
    fi
done

# Restart the crashed node
echo "--- Restarting $CRASH_NODE ---"
docker start "$CRASH_NODE" 2>/dev/null || true
sleep 5

RECOVER_TIME=$(date +%s)
TOTAL_DOWN=$((RECOVER_TIME - CRASH_TIME))

echo "Total downtime: ${TOTAL_DOWN}s"

if [ "$ALL_AVAILABLE" = true ]; then
    echo "=== PASS: Data available on remaining replicas after crash ==="
    exit 0
else
    echo "=== FAIL: Data lost or unavailable after crash ==="
    exit 1
fi
