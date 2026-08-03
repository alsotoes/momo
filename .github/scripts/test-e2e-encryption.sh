#!/bin/bash
set -e

# Resolves #577
# Usage: ./test-e2e-encryption.sh [momo-tcp|momo-quic]
# Tests E2EE encryption end-to-end:
#   1. Boots 3 daemons with encryption_enabled=true
#   2. Uploads a file via the client (content is encrypted client-side)
#   3. Verifies plaintext is NOT on any node's disk (zero-knowledge)
#   4. Verifies ciphertext IS on the primary node's disk (blob stored)
#   5. Verifies blob format starts with stream version byte 0x01
# Note: Replication to all nodes is tested by test-e2e.sh (without encryption).
# Chain placement depends on the ciphertext hash, so the primary may be the
# last node in the chain and not forward — only the primary is checked for blobs.
PROTOCOL=${1:-"momo-tcp"}

echo "Building Momo for E2E encryption tests ($PROTOCOL)..."
make build

echo "Setting up local directories..."
E2E_DIR="/tmp/momo-e2e-enc-$PROTOCOL"
rm -rf $E2E_DIR
mkdir -p $E2E_DIR/0 $E2E_DIR/1 $E2E_DIR/2

# Generate a random 256-bit encryption key (64 hex chars)
ENC_KEY=$(openssl rand -hex 32)
echo "Generated encryption key: $ENC_KEY"

cat << EOF > $E2E_DIR/e2e.conf
[global]
debug=true
auth_token=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6 # notsecret
replication_order=3,2,1
polymorphic_system=false
protocol=$PROTOCOL
encryption_enabled=true
encryption_key=$ENC_KEY
encryption_tenant=default
EOF

if [[ "$PROTOCOL" == *quic* ]]; then
  echo "tls_insecure=true" >> $E2E_DIR/e2e.conf
fi

cat << EOF >> $E2E_DIR/e2e.conf

[metrics]
interval=10
min_threshold=0.1
max_threshold=0.9
fallback_interval=30

[daemon.0]
host=127.0.0.1:4460
change_replication=127.0.0.1:5560
data=$E2E_DIR/0/
drive=/dev/sda1

[daemon.1]
host=127.0.0.1:4461
change_replication=127.0.0.1:5561
data=$E2E_DIR/1/
drive=/dev/sdb1

[daemon.2]
host=127.0.0.1:4462
change_replication=127.0.0.1:5562
data=$E2E_DIR/2/
drive=/dev/sdc1
EOF

echo "Starting local daemons with encryption enabled..."
./bin/momo -imp server -id 0 -config $E2E_DIR/e2e.conf > $E2E_DIR/s0.log 2>&1 &
P0=$!
./bin/momo -imp server -id 1 -config $E2E_DIR/e2e.conf > $E2E_DIR/s1.log 2>&1 &
P1=$!
./bin/momo -imp server -id 2 -config $E2E_DIR/e2e.conf > $E2E_DIR/s2.log 2>&1 &
P2=$!

trap "kill -9 $P0 $P1 $P2 || true; rm -rf $E2E_DIR" EXIT

echo "Waiting for daemons to bind..."
sleep 5

echo "Triggering replication mode change to Chain (1)..."
./bin/momo -imp repl -mode 1 -config $E2E_DIR/e2e.conf > $E2E_DIR/repl.log 2>&1
sleep 3

# Create test file with known plaintext content
TEST_PLAINTEXT="e2e-encryption-test-data-$PROTOCOL"
echo "$TEST_PLAINTEXT" > $E2E_DIR/test_enc_file.txt

echo "Running client to upload encrypted file..."
./bin/momo -imp client -file $E2E_DIR/test_enc_file.txt -config $E2E_DIR/e2e.conf > $E2E_DIR/client.log 2>&1

# Give it time to process and replicate (encrypted content is larger due to
# stream format overhead — 4KB chunk headers + 16-byte auth tags per chunk)
sleep 10

echo "Verifying encryption properties..."
FAIL=0

# 1. Plaintext must NOT be on any node's disk (zero-knowledge property)
for i in 0 1 2; do
  if grep -r -q "$TEST_PLAINTEXT" $E2E_DIR/$i/ 2>/dev/null; then
      echo "FAIL: Plaintext leaked to disk on Server $i ($PROTOCOL)"
      FAIL=1
  fi
done

# 2. Ciphertext (blob files) MUST exist on at least one node (the primary)
TOTAL_BLOBS=0
PRIMARY_SERVER=""
for i in 0 1 2; do
  BLOB_COUNT=$(find "$E2E_DIR/$i/blobs" -type f 2>/dev/null | wc -l)
  if [ "$BLOB_COUNT" -gt 0 ]; then
    TOTAL_BLOBS=$BLOB_COUNT
    PRIMARY_SERVER=$i
    break
  fi
done
if [ "$TOTAL_BLOBS" -eq 0 ]; then
  echo "FAIL: No blob files stored on any server ($PROTOCOL)"
  FAIL=1
fi

# 3. Blob content must start with stream version byte 0x01 (EncryptStream header)
if [ -n "$PRIMARY_SERVER" ]; then
  FIRST_BLOB=$(find "$E2E_DIR/$PRIMARY_SERVER/blobs" -type f 2>/dev/null | head -1)
  if [ -n "$FIRST_BLOB" ]; then
    FIRST_BYTE=$(xxd -l 1 -p "$FIRST_BLOB" 2>/dev/null)
    if [ "$FIRST_BYTE" != "01" ]; then
        echo "FAIL: Blob on Server $PRIMARY_SERVER does not start with stream version 0x01 (got 0x$FIRST_BYTE)"
        FAIL=1
    fi
  fi
fi

if [ $FAIL -eq 1 ]; then
  echo "--- DIRECTORY STRUCTURE ---"
  for i in 0 1 2; do
    echo "Server $i data dir:"
    find $E2E_DIR/$i/ -type f 2>/dev/null | head -20
  done
  echo "--- SERVER 0 LOG ---"
  cat $E2E_DIR/s0.log
  echo "--- SERVER 1 LOG ---"
  cat $E2E_DIR/s1.log
  echo "--- SERVER 2 LOG ---"
  cat $E2E_DIR/s2.log
  echo "--- CLIENT LOG ---"
  cat $E2E_DIR/client.log
  echo "--- REPL LOG ---"
  cat $E2E_DIR/repl.log
  exit 1
fi

echo "All encryption properties verified:"
echo "  - Plaintext NOT on any node's disk (zero-knowledge)"
echo "  - Ciphertext stored on primary node"
echo "  - Blob format correct (stream version 0x01 header)"
echo "E2E Encryption Test Passed ($PROTOCOL)!"
