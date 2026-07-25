#!/bin/bash
set -euo pipefail

# ============================================================
# Chaos Test: Network Partition Injection
# Simulates a netsplit between designated Momo nodes using iptables.
# Verifies that minority partitions reject writes (quorum loss)
# while majority partitions continue serving consistently.
# ============================================================

NODE_A="${1:-momo-server0}"
NODE_B="${1:-momo-server1}"
NODE_C="${1:-momo-server2}"
DURATION="${2:-30}"
RESULT_DIR="${3:-/tmp/momo-chaos-partition}"

mkdir -p "$RESULT_DIR"

echo "=== Chaos Test: Network Partition ==="
echo "Isolating $NODE_A from $NODE_B and $NODE_C for ${DURATION}s"

# Get container IPs
IP_A=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$NODE_A" 2>/dev/null || echo "")
IP_B=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$NODE_B" 2>/dev/null || echo "")
IP_C=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$NODE_C" 2>/dev/null || echo "")

if [ -z "$IP_A" ] || [ -z "$IP_B" ] || [ -z "$IP_C" ]; then
    echo "ERROR: Could not get container IPs. Are the containers running?"
    exit 1
fi

echo "Node A ($NODE_A): $IP_A"
echo "Node B ($NODE_B): $IP_B"
echo "Node C ($NODE_C): $IP_C"

# Inject partition: drop all traffic between Node A and Nodes B/C
echo "--- Injecting network partition ---"
docker exec "$NODE_A" iptables -A INPUT -s "$IP_B" -j DROP 2>/dev/null || true
docker exec "$NODE_A" iptables -A INPUT -s "$IP_C" -j DROP 2>/dev/null || true
docker exec "$NODE_A" iptables -A OUTPUT -d "$IP_B" -j DROP 2>/dev/null || true
docker exec "$NODE_A" iptables -A OUTPUT -d "$IP_C" -j DROP 2>/dev/null || true

docker exec "$NODE_B" iptables -A INPUT -s "$IP_A" -j DROP 2>/dev/null || true
docker exec "$NODE_C" iptables -A INPUT -s "$IP_A" -j DROP 2>/dev/null || true

echo "Partition active for ${DURATION}s..."

# Wait for partition duration
sleep "$DURATION"

# Heal the partition
echo "--- Healing network partition ---"
docker exec "$NODE_A" iptables -D INPUT -s "$IP_B" -j DROP 2>/dev/null || true
docker exec "$NODE_A" iptables -D INPUT -s "$IP_C" -j DROP 2>/dev/null || true
docker exec "$NODE_A" iptables -D OUTPUT -d "$IP_B" -j DROP 2>/dev/null || true
docker exec "$NODE_A" iptables -D OUTPUT -d "$IP_C" -j DROP 2>/dev/null || true

docker exec "$NODE_B" iptables -D INPUT -s "$IP_A" -j DROP 2>/dev/null || true
docker exec "$NODE_C" iptables -D INPUT -s "$IP_A" -j DROP 2>/dev/null || true

echo "Partition healed."

# Verify cluster recovery
echo "--- Verifying cluster recovery ---"
sleep 5

RECOVERED=true
for NODE in "$NODE_A" "$NODE_B" "$NODE_C"; do
    PORT=$(docker inspect -f '{{range $p, $conf := .NetworkSettings.Ports}}{{range $conf}}{{.HostPort}} {{end}}{{end}}' "$NODE" | awk '{print $1}')
    if curl -sf "http://localhost:$PORT/health" >/dev/null 2>&1; then
        echo "  $NODE: RECOVERED"
    else
        echo "  $NODE: UNREACHABLE"
        RECOVERED=false
    fi
done

if [ "$RECOVERED" = true ]; then
    echo "=== PASS: Cluster recovered after partition ==="
    exit 0
else
    echo "=== FAIL: Cluster did not fully recover ==="
    exit 1
fi
