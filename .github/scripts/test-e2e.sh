#!/bin/bash
set -e

echo "Building Momo for E2E tests..."
make build

echo "Setting up local directories..."
E2E_DIR="/tmp/momo-e2e"
rm -rf $E2E_DIR
mkdir -p $E2E_DIR/0 $E2E_DIR/1 $E2E_DIR/2

cat << 'EOF' > $E2E_DIR/e2e.conf
[global]
debug=true
auth_token=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6
replication_order=4,3,2,1
polymorphic_system=false

[metrics]
interval=10
min_threshold=0.1
max_threshold=0.9
fallback_interval=30

[daemon.0]
host=127.0.0.1:4440
change_replication=127.0.0.1:5550
data=/tmp/momo-e2e/0/
drive=/dev/sda1

[daemon.1]
host=127.0.0.1:4441
change_replication=127.0.0.1:5551
data=/tmp/momo-e2e/1/
drive=/dev/sdb1

[daemon.2]
host=127.0.0.1:4442
change_replication=127.0.0.1:5552
data=/tmp/momo-e2e/2/
drive=/dev/sdc1
EOF

echo "Starting local daemons..."
./bin/momo -imp server -id 0 -config $E2E_DIR/e2e.conf > $E2E_DIR/s0.log 2>&1 &
P0=$!
./bin/momo -imp server -id 1 -config $E2E_DIR/e2e.conf > $E2E_DIR/s1.log 2>&1 &
P1=$!
./bin/momo -imp server -id 2 -config $E2E_DIR/e2e.conf > $E2E_DIR/s2.log 2>&1 &
P2=$!

# Ensure cleanup on exit
trap "kill -9 $P0 $P1 $P2 || true; rm -rf $E2E_DIR" EXIT

echo "Waiting for daemons to bind..."
sleep 3

# Switch replication to Chain (1) to ensure data reaches all nodes
echo "Triggering replication mode change to Chain (1)..."
TS=$(date +%s%N)
echo "{\"old\":4,\"new\":1,\"timestamp\":$TS}" > $E2E_DIR/repl.json
printf "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" | cat - $E2E_DIR/repl.json | nc 127.0.0.1 5550
sleep 1

# Create dummy file to send
echo "e2etestdata" > $E2E_DIR/test_e2e_file.txt

# Run client
echo "Running client to upload file..."
./bin/momo -imp client -file $E2E_DIR/test_e2e_file.txt -config $E2E_DIR/e2e.conf > $E2E_DIR/client.log 2>&1

# Give it a second to process and replicate
sleep 3

echo "Checking data consistency across nodes..."
FAIL=0

for i in 0 1 2; do
  if ! grep -q "e2etestdata" $E2E_DIR/$i/test_e2e_file.txt 2>/dev/null; then
      echo "E2E Test Failed: Data not found or mismatched on Server $i"
      FAIL=1
  fi
done

if [ $FAIL -eq 1 ]; then
  echo "--- SERVER 0 LOG ---"
  cat $E2E_DIR/s0.log
  echo "--- SERVER 1 LOG ---"
  cat $E2E_DIR/s1.log
  echo "--- SERVER 2 LOG ---"
  cat $E2E_DIR/s2.log
  echo "--- CLIENT LOG ---"
  cat $E2E_DIR/client.log
  exit 1
fi

echo "Data is consistent across all servers. E2E Test Passed!"