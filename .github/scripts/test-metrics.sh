#!/bin/bash
set -e

# Test that the Prometheus metrics endpoint works correctly.
# 1. Starts a 1-node momo cluster with prometheus_port enabled
# 2. Uploads a file via the momo client
# 3. Scrapes /metrics and verifies Prometheus format + counter values
# 4. Scrapes /health endpoint

echo "=== Metrics E2E Test ==="

# Build
echo "Building momo..."
make build

# Setup
TEST_DIR="/tmp/momo-metrics-test"
rm -rf "$TEST_DIR"
mkdir -p "$TEST_DIR/data"

cat << EOF > "$TEST_DIR/metrics.conf"
[global]
debug=false
auth_token=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6 # notsecret
replication_order=3,2,1
polymorphic_system=false
protocol=momo-tcp

[metrics]
interval=10
min_threshold=0.1
max_threshold=0.9
fallback_interval=30
prometheus_port=9199

[daemon.0]
host=127.0.0.1:4440
change_replication=127.0.0.1:5550
data=$TEST_DIR/data/
drive=/dev/sda1
EOF

# Start server
echo "Starting momo server with metrics on :9199..."
./bin/momo -imp server -id 0 -config "$TEST_DIR/metrics.conf" > "$TEST_DIR/server.log" 2>&1 &
SERVER_PID=$!

trap "kill -9 $SERVER_PID 2>/dev/null || true; rm -rf $TEST_DIR" EXIT

echo "Waiting for server to bind..."
sleep 3

# Verify /health endpoint
echo "--- Testing /health endpoint ---"
HEALTH_RESP=$(curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:9199/health || true)
if [ "$HEALTH_RESP" != "200" ]; then
    echo "FAIL: /health returned $HEALTH_RESP, expected 200"
    cat "$TEST_DIR/server.log"
    exit 1
fi
echo "PASS: /health returned 200"

HEALTH_BODY=$(curl -s http://127.0.0.1:9199/health)
if [ "$HEALTH_BODY" != "OK" ]; then
    echo "FAIL: /health body was '$HEALTH_BODY', expected 'OK'"
    exit 1
fi
echo "PASS: /health body is 'OK'"

# Scrape /metrics before upload — verify format and zero counters
echo "--- Testing /metrics before upload ---"
METRICS_BEFORE=$(curl -s http://127.0.0.1:9199/metrics)

# Check Prometheus format markers
if ! echo "$METRICS_BEFORE" | grep -q "^# HELP momo_"; then
    echo "FAIL: /metrics missing # HELP lines (not Prometheus format)"
    echo "$METRICS_BEFORE"
    exit 1
fi
echo "PASS: /metrics has # HELP lines (Prometheus format)"

if ! echo "$METRICS_BEFORE" | grep -q "^# TYPE momo_"; then
    echo "FAIL: /metrics missing # TYPE lines"
    exit 1
fi
echo "PASS: /metrics has # TYPE lines"

# Check specific metrics exist
for METRIC in momo_connections_total momo_active_connections momo_uploads_total momo_downloads_total momo_deletes_total momo_replication_total momo_errors_total momo_bytes_uploaded_total momo_bytes_downloaded_total momo_uptime_seconds momo_goroutines momo_memory_alloc_bytes momo_memory_sys_bytes momo_gc_runs_total momo_build_info; do
    if ! echo "$METRICS_BEFORE" | grep -q "^$METRIC"; then
        echo "FAIL: /metrics missing $METRIC"
        exit 1
    fi
done
echo "PASS: All 15 expected metrics present"

# Check initial counter values
CONNS_BEFORE=$(echo "$METRICS_BEFORE" | grep "^momo_connections_total " | awk '{print $2}')
UPLOADS_BEFORE=$(echo "$METRICS_BEFORE" | grep "^momo_uploads_total " | awk '{print $2}')
if [ "$CONNS_BEFORE" != "0" ]; then
    echo "FAIL: momo_connections_total=$CONNS_BEFORE before any traffic, expected 0"
    exit 1
fi
echo "PASS: momo_connections_total=0 before upload"

# Upload a file
echo "--- Uploading test file ---"
echo "metrics-test-data-content" > "$TEST_DIR/testfile.txt"
./bin/momo -imp client -file "$TEST_DIR/testfile.txt" -config "$TEST_DIR/metrics.conf" > "$TEST_DIR/client.log" 2>&1 || true
sleep 2

# Scrape /metrics after upload — verify counters incremented
echo "--- Testing /metrics after upload ---"
METRICS_AFTER=$(curl -s http://127.0.0.1:9199/metrics)

UPLOADS_AFTER=$(echo "$METRICS_AFTER" | grep "^momo_uploads_total " | awk '{print $2}')
CONNS_AFTER=$(echo "$METRICS_AFTER" | grep "^momo_connections_total " | awk '{print $2}')
BYTES_UP=$(echo "$METRICS_AFTER" | grep "^momo_bytes_uploaded_total " | awk '{print $2}')

if [ "$UPLOADS_AFTER" -lt 1 ] 2>/dev/null; then
    echo "FAIL: momo_uploads_total=$UPLOADS_AFTER after upload, expected >= 1"
    echo "$METRICS_AFTER"
    cat "$TEST_DIR/client.log"
    exit 1
fi
echo "PASS: momo_uploads_total=$UPLOADS_AFTER (incremented after upload)"

if [ "$CONNS_AFTER" -lt 1 ] 2>/dev/null; then
    echo "FAIL: momo_connections_total=$CONNS_AFTER after upload, expected >= 1"
    exit 1
fi
echo "PASS: momo_connections_total=$CONNS_AFTER (incremented after upload)"

if [ "$BYTES_UP" -lt 1 ] 2>/dev/null; then
    echo "FAIL: momo_bytes_uploaded_total=$BYTES_UP after upload, expected >= 1"
    exit 1
fi
echo "PASS: momo_bytes_uploaded_total=$BYTES_UP (bytes recorded)"

# Verify errors is still 0 (no errors expected)
ERRORS_AFTER=$(echo "$METRICS_AFTER" | grep "^momo_errors_total " | awk '{print $2}')
if [ "$ERRORS_AFTER" != "0" ]; then
    echo "WARN: momo_errors_total=$ERRORS_AFTER (expected 0 for clean upload)"
fi

# Verify uptime is a positive float
UPTIME=$(echo "$METRICS_AFTER" | grep "^momo_uptime_seconds " | awk '{print $2}')
if [ -z "$UPTIME" ] || [ "$UPTIME" = "0.00" ]; then
    echo "FAIL: momo_uptime_seconds=$UPTIME, expected positive value"
    exit 1
fi
echo "PASS: momo_uptime_seconds=$UPTIME"

# Verify build_info has hostname label
if ! echo "$METRICS_AFTER" | grep -q 'momo_build_info{hostname='; then
    echo "FAIL: momo_build_info missing hostname label"
    exit 1
fi
echo "PASS: momo_build_info has hostname label"

echo ""
echo "=== All metrics tests passed! ==="
echo "Final metrics snapshot:"
echo "$METRICS_AFTER"
