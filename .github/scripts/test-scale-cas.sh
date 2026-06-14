#!/bin/bash
set -e

echo "Building Momo for Scale/CAS tests..."
make build

E2E_DIR="/tmp/momo-scale-cas"
rm -rf $E2E_DIR
mkdir -p $E2E_DIR/0 $E2E_DIR/1 $E2E_DIR/2 $E2E_DIR/3 $E2E_DIR/4

cat << EOF > $E2E_DIR/e2e.conf
[global]
debug=true
auth_token=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6
replication_order=4,3,2,1
replication_factor=3
polymorphic_system=false
protocol=momo-tcp

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

[daemon.3]
host=127.0.0.1:4443
change_replication=127.0.0.1:5553
data=$E2E_DIR/3/
drive=/dev/sdd1

[daemon.4]
host=127.0.0.1:4444
change_replication=127.0.0.1:5554
data=$E2E_DIR/4/
drive=/dev/sde1
EOF

echo "Starting 5 virtual daemons..."
./bin/momo -imp server -id 0 -config $E2E_DIR/e2e.conf > $E2E_DIR/s0.log 2>&1 &
P0=$!
./bin/momo -imp server -id 1 -config $E2E_DIR/e2e.conf > $E2E_DIR/s1.log 2>&1 &
P1=$!
./bin/momo -imp server -id 2 -config $E2E_DIR/e2e.conf > $E2E_DIR/s2.log 2>&1 &
P2=$!
./bin/momo -imp server -id 3 -config $E2E_DIR/e2e.conf > $E2E_DIR/s3.log 2>&1 &
P3=$!
./bin/momo -imp server -id 4 -config $E2E_DIR/e2e.conf > $E2E_DIR/s4.log 2>&1 &
P4=$!

# Ensure cleanup
trap "kill -9 $P0 $P1 $P2 $P3 $P4 || true; rm -rf $E2E_DIR" EXIT

echo "Waiting for daemons to bind..."
sleep 5

# Ensure we are in a replication mode that uses secondaries (e.g., Chain)
echo "Setting cluster to Chain Replication..."
./bin/momo -imp repl -mode 1 -config $E2E_DIR/e2e.conf > $E2E_DIR/repl.log 2>&1
sleep 2

# 1. Test sending multiple tiny files
echo "Uploading multiple files..."
echo "content1" > $E2E_DIR/file1.txt
echo "content2" > $E2E_DIR/file2.txt
echo "content3" > $E2E_DIR/file3.txt

./bin/momo -imp client -file $E2E_DIR/file1.txt -config $E2E_DIR/e2e.conf >> $E2E_DIR/client.log 2>&1
./bin/momo -imp client -file $E2E_DIR/file2.txt -config $E2E_DIR/e2e.conf >> $E2E_DIR/client.log 2>&1
./bin/momo -imp client -file $E2E_DIR/file3.txt -config $E2E_DIR/e2e.conf >> $E2E_DIR/client.log 2>&1

sleep 3

# 2. Test Deduplication
echo "Testing CAS Deduplication (uploading file1 content again as duplicate.txt)..."
echo "content1" > $E2E_DIR/duplicate.txt
./bin/momo -imp client -file $E2E_DIR/duplicate.txt -config $E2E_DIR/e2e.conf >> $E2E_DIR/client.log 2>&1

sleep 2

# Verification: Check if deduplication was logged by the primary node of file1
echo "Verifying Deduplication logs..."
if ! grep -q "Deduplication hit" $E2E_DIR/s*.log; then
    echo "FAILED: Deduplication hit not found in server logs"
    exit 1
fi

# Verification: Check replication factor enforcement (each file should be in 3 nodes)
echo "Verifying Replication Factor (3 copies per file)..."
for f in "content1" "content2" "content3"; do
    COUNT=$(grep -r -l "$f" $E2E_DIR/[0-4]/ | wc -l)
    if [ "$COUNT" -ne 3 ]; then
        echo "FAILED: Expected 3 copies of $f, found $COUNT"
        # Print logs for debugging
        cat $E2E_DIR/client.log
        exit 1
    fi
done

echo "Scale & CAS E2E Test Passed! 5 Nodes, Factor 3, Deduplication Verified."
