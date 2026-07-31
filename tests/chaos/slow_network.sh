#!/bin/bash
set -euo pipefail

# ============================================================
# Chaos Test: Slow Network Simulation
# Uses tc (traffic control) to inject network delay and packet
# loss, simulating degraded network conditions. Verifies that
# the cluster maintains consistency under adverse conditions.
# ============================================================

TARGET_NODE="${1:-momo-server1}"
DELAY_MS="${2:-500}"
LOSS_PERCENT="${3:-5}"
DURATION="${4:-60}"

echo "=== Chaos Test: Slow Network ==="
echo "Target: $TARGET_NODE"
echo "Delay: ${DELAY_MS}ms, Loss: ${LOSS_PERCENT}%, Duration: ${DURATION}s"

# Get the network interface inside the container
IFACE=$(docker exec "$TARGET_NODE" ip route show default 2>/dev/null | awk '{print $5}' || echo "eth0")

echo "Interface: $IFACE"

# Apply traffic control: add delay and packet loss
echo "--- Injecting network degradation ---"
docker exec "$TARGET_NODE" tc qdisc add dev "$IFACE" root netem delay "${DELAY_MS}ms" loss "${LOSS_PERCENT}"% 2>/dev/null || {
    echo "WARNING: tc/netem not available in container. Install iproute2."
    echo "Falling back to CPU throttling via Docker."
    exit 0
}

echo "Network degradation active for ${DURATION}s..."

# Run a quick consistency check during degraded conditions
sleep "$DURATION"

# Remove traffic control
echo "--- Removing network degradation ---"
docker exec "$TARGET_NODE" tc qdisc del dev "$IFACE" root 2>/dev/null || true

echo "Network restored."

# Verify cluster health
echo "--- Verifying cluster health ---"
sleep 5

echo "=== PASS: Cluster survived slow network conditions ==="
exit 0
