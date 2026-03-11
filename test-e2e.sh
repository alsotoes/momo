#!/bin/bash
set -e

echo "Starting Docker Compose environment for E2E tests..."
docker-compose up --build -d

echo "Waiting for momo-server0 to become healthy..."
# We wait for up to 30 seconds
for i in {1..15}; do
  if docker inspect --format="{{.State.Health.Status}}" momo-server0 | grep -q "healthy"; then
    echo "Servers are healthy."
    break
  fi
  echo "Waiting..."
  sleep 2
done

# Create dummy file to send
echo "e2etestdata" > test_e2e_file.txt

# Create client container and connect to server0
echo "Running client container to upload file..."
docker-compose run --rm client -imp client -file /files/test_e2e_file.txt -config conf/momo.conf

# Give it a second to process and replicate
sleep 3

echo "Checking data consistency across nodes..."
# Server0
if ! docker exec momo-server0 sh -c "cat /root/received_files/dir1/test_e2e_file.txt | grep e2etestdata"; then
    echo "E2E Test Failed: Data not found on Server 0"
    docker-compose logs
    exit 1
fi

# Server1
if ! docker exec momo-server1 sh -c "cat /root/received_files/dir2/test_e2e_file.txt | grep e2etestdata"; then
    echo "E2E Test Failed: Data not found on Server 1 (Replication Failed)"
    docker-compose logs
    exit 1
fi

# Server2
if ! docker exec momo-server2 sh -c "cat /root/received_files/dir3/test_e2e_file.txt | grep e2etestdata"; then
    echo "E2E Test Failed: Data not found on Server 2 (Replication Failed)"
    docker-compose logs
    exit 1
fi

echo "Data is consistent across all servers. E2E Test Passed!"

echo "Tearing down Docker Compose environment..."
docker-compose down -v
rm test_e2e_file.txt