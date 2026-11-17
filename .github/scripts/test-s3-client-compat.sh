#!/bin/bash
# Verifies the S3 gateway against a real S3 client (aws-cli) instead of hand-made
# curl requests. Catches the class of bug where the gateway accepts a request but
# the client cannot read the object back (issues #1346, #1348).
#
# Usage: ./test-s3-client-compat.sh [port-base]
set -euo pipefail

PORT_BASE=${1:-7400}
DIR=$(mktemp -d /tmp/momo-s3-client-XXXXXX)
MOMO=./bin/momo
BUCKET=testb
KEY=f1.bin

cleanup() {
  for pid in ${PIDS:-}; do kill "$pid" 2>/dev/null || true; done
  rm -rf "$DIR"
}
trap cleanup EXIT

if ! command -v aws >/dev/null 2>&1; then
  echo "SKIP: aws-cli not installed"
  exit 0
fi

echo "Building momo..."
make build >/dev/null

mkdir -p "$DIR"/{0,1,2}
cat > "$DIR/momo.conf" <<CONF
[global]
debug=false
auth_token=compat-token # notsecret
replication_order=3,2,1
polymorphic_system=false
protocol=s3-tcp
tls_insecure=true
replication_factor=3

[metrics]
interval=10
min_threshold=0.1
max_threshold=0.9
fallback_interval=30

[daemon.0]
host=127.0.0.1:$((PORT_BASE + 0))
change_replication=127.0.0.1:$((PORT_BASE + 100))
data=$DIR/0/
drive=/dev/sda1

[daemon.1]
host=127.0.0.1:$((PORT_BASE + 1))
change_replication=127.0.0.1:$((PORT_BASE + 101))
data=$DIR/1/
drive=/dev/sdb1

[daemon.2]
host=127.0.0.1:$((PORT_BASE + 2))
change_replication=127.0.0.1:$((PORT_BASE + 102))
data=$DIR/2/
drive=/dev/sdc1
CONF

PIDS=""
for i in 0 1 2; do
  $MOMO -imp server -id "$i" -config "$DIR/momo.conf" > "$DIR/s$i.log" 2>&1 &
  PIDS="$PIDS $!"
done

echo "Waiting for the cluster to bind..."
for _ in $(seq 1 30); do
  if (exec 3<>/dev/tcp/127.0.0.1/$((PORT_BASE + 0))) 2>/dev/null; then
    exec 3<&- 3>&-
    break
  fi
  sleep 1
done

export AWS_ACCESS_KEY_ID=compat-token
export AWS_SECRET_ACCESS_KEY=compat-token
export AWS_DEFAULT_REGION=us-east-1
export AWS_EC2_METADATA_DISABLED=true
ENDPOINT="--endpoint-url http://127.0.0.1:$((PORT_BASE + 0))"

fail() { echo "FAIL: $*"; for i in 0 1 2; do echo "--- s$i.log ---"; tail -20 "$DIR/s$i.log"; done; exit 1; }

echo "1/7 put-object"
dd if=/dev/urandom of="$DIR/$KEY" bs=1M count=1 2>/dev/null
aws $ENDPOINT s3api put-object --bucket "$BUCKET" --key "$KEY" --body "$DIR/$KEY" >/dev/null || fail "put-object"

echo "2/7 head-object"
aws $ENDPOINT s3api head-object --bucket "$BUCKET" --key "$KEY" >/dev/null || fail "head-object"

echo "3/7 get-object"
aws $ENDPOINT s3api get-object --bucket "$BUCKET" --key "$KEY" "$DIR/out.bin" >/dev/null || fail "get-object"
cmp "$DIR/$KEY" "$DIR/out.bin" || fail "downloaded bytes differ from the uploaded object"

echo "4/7 list-objects-v2 reports the key only"
LISTED=$(aws $ENDPOINT s3api list-objects-v2 --bucket "$BUCKET" --query 'Contents[].Key' --output text)
[ "$LISTED" = "$KEY" ] || fail "listed keys '$LISTED', expected '$KEY' (bucket must not be part of the key)"

echo "5/7 copy-object"
aws $ENDPOINT s3api copy-object --bucket "$BUCKET" --key copy.bin --copy-source "$BUCKET/$KEY" >/dev/null || fail "copy-object"
aws $ENDPOINT s3api head-object --bucket "$BUCKET" --key copy.bin >/dev/null || fail "head-object on the copy"

echo "6/7 multipart upload (aws s3 cp, 20 MiB)"
dd if=/dev/urandom of="$DIR/big.bin" bs=1M count=20 2>/dev/null
aws $ENDPOINT s3 cp "$DIR/big.bin" "s3://$BUCKET/big.bin" --only-show-errors || fail "multipart upload"
aws $ENDPOINT s3 cp "s3://$BUCKET/big.bin" "$DIR/big.out" --only-show-errors || fail "multipart download"
cmp "$DIR/big.bin" "$DIR/big.out" || fail "multipart round-trip differs"

echo "7/7 delete-object"
aws $ENDPOINT s3api delete-object --bucket "$BUCKET" --key copy.bin >/dev/null || fail "delete-object"
if aws $ENDPOINT s3api head-object --bucket "$BUCKET" --key copy.bin >/dev/null 2>&1; then
  fail "deleted object is still readable"
fi

echo "S3 client compatibility test passed (aws-cli)"
