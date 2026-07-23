#!/bin/bash
set -e

# E2E test for P2P gossip membership across separate momo server processes.
# Verifies that nodes discover each other via gossip and detect failures.

echo "Building Momo for P2P E2E test..."
make build

echo "Setting up local directories..."
E2E_DIR="/tmp/momo-e2e-p2p"
rm -rf $E2E_DIR
mkdir -p $E2E_DIR/0 $E2E_DIR/1 $E2E_DIR/2

cat << EOF > $E2E_DIR/e2e.conf
[global]
debug=true
auth_token=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6
replication_order=3,2,1
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

[p2p]
enabled=true
gossip_port=4450
gossip_interval=1
suspicion_timeout=5
fanout=3
EOF

echo "Starting 3 daemons with P2P enabled..."
./bin/momo -imp server -id 0 -config $E2E_DIR/e2e.conf > $E2E_DIR/s0.log 2>&1 &
P0=$!
./bin/momo -imp server -id 1 -config $E2E_DIR/e2e.conf > $E2E_DIR/s1.log 2>&1 &
P1=$!
./bin/momo -imp server -id 2 -config $E2E_DIR/e2e.conf > $E2E_DIR/s2.log 2>&1 &
P2=$!

trap "kill -9 $P0 $P1 $P2 || true; rm -rf $E2E_DIR" EXIT

echo "Waiting for daemons to start and gossip to converge..."
sleep 8

echo "Checking gossip membership convergence..."
FAIL=0

for i in 0 1 2; do
  PEER_COUNT=$(grep -c "P2P: gossip started" $E2E_DIR/s$i.log 2>/dev/null || echo 0)
  if [ "$PEER_COUNT" -eq 0 ]; then
    echo "FAIL: Node $i did not start P2P gossip"
    FAIL=1
  fi
done

if [ $FAIL -eq 1 ]; then
  for i in 0 1 2; do echo "--- SERVER $i LOG ---"; cat $E2E_DIR/s$i.log; done
  exit 1
fi

echo "Checking that nodes discovered each other via gossip..."
DISCOVERY_COUNT=0
for i in 0 1 2; do
  DISCOVERED=$(grep -c "discovered new peer\|peer.*connected" $E2E_DIR/s$i.log 2>/dev/null || echo 0)
  echo "Node $i discovered $DISCOVERED peer connections"
  DISCOVERY_COUNT=$((DISCOVERY_COUNT + DISCOVERED))
done

if [ "$DISCOVERY_COUNT" -lt 3 ]; then
  echo "FAIL: Expected at least 3 total peer discoveries, got $DISCOVERY_COUNT"
  for i in 0 1 2; do echo "--- SERVER $i LOG ---"; cat $E2E_DIR/s$i.log; done
  exit 1
fi

echo "Killing node 2 to test failure detection..."
kill -9 $P2
P2=""

echo "Waiting for suspicion timeout and convergence..."
sleep 12

echo "Checking that nodes >0 marked node 2 as suspect or offline..."
SUSPECT_FOUND=0
for i in 0 1; do
  SUSPECT=$(grep -c "SUSPECT\|OFFLINE" $E2E_DIR/s$i.log 2>/dev/null || echo 0)
  echo "Node $i suspicion events: $SUSPECT"
  SUSPECT_FOUND=$((SUSPECT_FOUND + SUSPECT))
done

if [ "$SUSPECT_FOUND" -eq 0 ]; then
  echo "FAIL: No suspicion events detected after node failure"
  for i in 0 1; do echo "--- SERVER $i LOG ---"; cat $E2E_DIR/s$i.log; done
  exit 1
fi

echo "P2P E2E Test Passed: gossip membership + failure detection working!"
