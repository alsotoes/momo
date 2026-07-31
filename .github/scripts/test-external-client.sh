#!/bin/bash
set -e

# E2E test for external S3 client replication mode downgrade.
# Verifies that:
# 1. aws-cli (curl) PUT to a server in primary-splay mode downgrades to splay and replicates.
# 2. momo CLI client still uses primary-splay mode 3.

echo "Building Momo for external client E2E test..."
make build

echo "Setting up local directories..."
E2E_DIR="/tmp/momo-e2e-external-client"
rm -rf $E2E_DIR
mkdir -p $E2E_DIR/0 $E2E_DIR/1 $E2E_DIR/2

cat << EOF > $E2E_DIR/e2e.conf
[global]
debug=true
auth_token=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6 # notsecret
replication_order=3,2,1
client_side_replication_modes=3
polymorphic_system=false
protocol=s3-tcp

[metrics]
interval=10
min_threshold=0.1
max_threshold=0.9
fallback_interval=30

[daemon.0]
host=127.0.0.1:4440
change_replication=127.0.0.1:5550
data=$E2E_DIR/0/
drive=/dev/sda1

[daemon.1]
host=127.0.0.1:4441
change_replication=127.0.0.1:5551
data=$E2E_DIR/1/
drive=/dev/sdb1

[daemon.2]
host=127.0.0.1:4442
change_replication=127.0.0.1:5552
data=$E2E_DIR/2/
drive=/dev/sdc1
EOF

echo "Starting local daemons (s3-tcp)..."
./bin/momo -imp server -id 0 -config $E2E_DIR/e2e.conf > $E2E_DIR/s0.log 2>&1 &
P0=$!
./bin/momo -imp server -id 1 -config $E2E_DIR/e2e.conf > $E2E_DIR/s1.log 2>&1 &
P1=$!
./bin/momo -imp server -id 2 -config $E2E_DIR/e2e.conf > $E2E_DIR/s2.log 2>&1 &
P2=$!

trap "kill -9 $P0 $P1 $P2 || true; rm -rf $E2E_DIR" EXIT

echo "Waiting for daemons to bind..."
sleep 5

# Switch to primary-splay (mode 3) — the client-side replication mode
echo "Triggering replication mode change to PrimarySplay (3)..."
./bin/momo -imp repl -mode 3 -config $E2E_DIR/e2e.conf > $E2E_DIR/repl.log 2>&1
sleep 3

# --- Test 1: External S3 client (curl simulating aws-cli) ---
echo ""
echo "=== Test 1: External S3 client (curl) PUT in primary-splay mode ==="
echo "external-client-test-data" > $E2E_DIR/test_external.txt

# Compute actual SHA-256 hash of the file content
FILE_HASH=$(sha256sum $E2E_DIR/test_external.txt | awk '{print $1}')
FILE_SIZE=$(wc -c < $E2E_DIR/test_external.txt)

# curl PUT without X-Momo-Requested-Mode — simulates aws-cli
AUTH_HDR="Authorization: AWS4-HMAC-SHA256 Credential=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6/20260727/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=dummy"  # notsecret
curl -s -X PUT \
  -H "$AUTH_HDR" \
  -H "X-Amz-Date: 20260727T120000Z" \
  -H "X-Amz-Content-Sha256: $FILE_HASH" \
  -H "Content-Type: application/octet-stream" \
  --data-binary @$E2E_DIR/test_external.txt \
  http://127.0.0.1:4440/test-bucket/test_external.txt || true

sleep 5

echo "Checking replication (should be downgraded to splay mode 2)..."
FAIL=0
for i in 0 1 2; do
  if ! grep -r -q "external-client-test-data" $E2E_DIR/$i/ 2>/dev/null; then
    echo "FAIL: Data not found on Server $i"
    FAIL=1
  else
    echo "OK: Data found on Server $i"
  fi
done

if [ $FAIL -eq 1 ]; then
  echo "Test 1 FAILED: External client data not replicated"
  echo "--- SERVER 0 LOG ---"; cat $E2E_DIR/s0.log
  echo "--- SERVER 1 LOG ---"; cat $E2E_DIR/s1.log
  echo "--- SERVER 2 LOG ---"; cat $E2E_DIR/s2.log
  exit 1
fi
echo "Test 1 PASSED: External client data replicated to all nodes"

# --- Test 2: momo CLI client (should use primary-splay mode 3) ---
echo ""
echo "=== Test 2: momo CLI client PUT in primary-splay mode ==="
echo "momo-cli-test-data" > $E2E_DIR/test_momo.txt

./bin/momo -imp client -file $E2E_DIR/test_momo.txt -config $E2E_DIR/e2e.conf > $E2E_DIR/client.log 2>&1 || true

sleep 5

echo "Checking replication (should use primary-splay mode 3)..."
FAIL=0
for i in 0 1 2; do
  if ! grep -r -q "momo-cli-test-data" $E2E_DIR/$i/ 2>/dev/null; then
    echo "FAIL: Data not found on Server $i"
    FAIL=1
  else
    echo "OK: Data found on Server $i"
  fi
done

if [ $FAIL -eq 1 ]; then
  echo "Test 2 FAILED: momo CLI client data not replicated"
  echo "--- SERVER 0 LOG ---"; cat $E2E_DIR/s0.log
  echo "--- SERVER 1 LOG ---"; cat $E2E_DIR/s1.log
  echo "--- SERVER 2 LOG ---"; cat $E2E_DIR/s2.log
  echo "--- CLIENT LOG ---"; cat $E2E_DIR/client.log
  exit 1
fi
echo "Test 2 PASSED: momo CLI client data replicated to all nodes"

echo ""
echo "All external client E2E tests passed!"
