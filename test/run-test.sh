#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
TEST_DATA_DIR="$SCRIPT_DIR/data"
MP4_FILE="$TEST_DATA_DIR/test.mp4"
MP4_HASH_FILE="$TEST_DATA_DIR/test.mp4.sha256"

PORTAL=http://localhost:8080

cd "$PROJECT_DIR"

echo "============================================"
echo "  Large File Transfer Integration Test"
echo "============================================"

cleanup() {
    echo ""
    echo "=== Cleaning up ==="
    docker compose -f docker-compose.test.yml down -v 2>/dev/null || true
    if [ -d "$TEST_DATA_DIR" ]; then
        rm -rf "$TEST_DATA_DIR"
    fi
    echo "Cleanup done"
}
trap cleanup EXIT

mkdir -p "$TEST_DATA_DIR"

echo ""
echo "--- Phase 1: Building binaries and Docker images ---"
echo "Building entry binary (static)..."
CGO_ENABLED=0 go build -o docker/test-entry/entry ./cmd/entry/
echo "Building worker binary (static)..."
CGO_ENABLED=0 go build -o docker/test-worker/worker ./cmd/worker/
rm -rf docker/test-entry/migrations docker/test-worker/migrations
cp -a migrations docker/test-entry/migrations
cp -a migrations docker/test-worker/migrations
echo "Building Docker images..."
docker build -t test-entry:latest ./docker/test-entry/
docker build -t test-worker:latest ./docker/test-worker/
docker build -t data-service:latest ./docker/fake-services/data-service/

echo ""
echo "--- Phase 2: Generating 2GB MP4 test file ---"
rm -rf "$TEST_DATA_DIR"
mkdir -p "$TEST_DATA_DIR"
go run test/generate-mp4.go
EXPECTED_HASH=$(cat "$MP4_HASH_FILE")
echo "Expected SHA256: $EXPECTED_HASH"

echo ""
echo "--- Phase 2b: Seeding deployable service config ---"
mkdir -p services/data-service
printf 'config placeholder\n' > services/data-service/config.yaml

echo ""
echo "--- Phase 3: Starting Docker Compose ---"
docker compose -f docker-compose.test.yml up -d

echo "Waiting for services to be ready..."
sleep 10

echo "Checking NATS..."
for i in $(seq 1 30); do
    if docker exec test-nats nats-server --version >/dev/null 2>&1 || true; then
        break
    fi
    sleep 1
done

echo "Checking MariaDB..."
MARIADB_READY=0
for i in $(seq 1 60); do
    if docker exec test-mariadb healthcheck.sh --connect --innodb_initialized >/dev/null 2>&1; then
        echo "MariaDB is ready"
        MARIADB_READY=1
        break
    fi
    echo "Waiting for MariaDB... ($i)"
    sleep 2
done
if [ "$MARIADB_READY" != "1" ]; then
    echo "MariaDB did not become ready"
    exit 1
fi

echo "Waiting for entry service..."
ENTRY_READY=0
for i in $(seq 1 30); do
    if curl -sf "$PORTAL/health" >/dev/null 2>&1; then
        echo "Entry service is ready"
        ENTRY_READY=1
        break
    fi
    echo "Waiting for entry... ($i)"
    if [ $((i % 5)) -eq 0 ]; then
        docker logs test-entry 2>&1 | tail -5 || true
    fi
    sleep 2
done
if [ "$ENTRY_READY" != "1" ]; then
    echo "Entry service did not become ready"
    docker logs test-entry 2>&1 | tail -50 || true
    exit 1
fi

echo "Waiting for worker node to register..."
NODE_STATUS=""
for i in $(seq 1 30); do
    NODE_STATUS=$(curl -s "$PORTAL/nodes/node-1" | jq -r .status 2>/dev/null || echo "")
    if [ "$NODE_STATUS" = "ONLINE" ]; then
        echo "Node node-1 is ONLINE"
        break
    fi
    echo "Waiting for node... ($NODE_STATUS) ($i)"
    sleep 2
done
if [ "$NODE_STATUS" != "ONLINE" ]; then
    echo "Worker node did not come online"
    curl -s "$PORTAL/nodes" || true
    exit 1
fi

echo ""
echo "--- Phase 4: Creating operation and uploading 2GB file via tus ---"
echo "Creating operation..."
OP_ID=$(curl -s -X POST "$PORTAL/updates" \
    -H "Content-Type: application/json" \
    -d '{"service":"data-service","node_id":"node-1"}' | jq -r .operation_id)
echo "Operation ID: $OP_ID"

FILE_SIZE=$(stat -c%s "$MP4_FILE")
OP_B64=$(printf '%s' "$OP_ID" | base64 | tr -d '\n')
FN_B64=$(printf '%s' "test.mp4" | base64 | tr -d '\n')
CHUNK_SIZE=$((64 * 1024 * 1024))

echo "Creating tus upload (Upload-Length: $FILE_SIZE)..."
CREATE_HEADERS=$(curl -si -X POST "$PORTAL/uploads" \
    -H "Tus-Resumable: 1.0.0" \
    -H "Upload-Length: $FILE_SIZE" \
    -H "Upload-Metadata: operation_id ${OP_B64},filename ${FN_B64}")
LOCATION=$(printf '%s' "$CREATE_HEADERS" | tr -d '\r' | awk -F': ' 'tolower($1)=="location"{print $2; exit}')
if [ -z "$LOCATION" ]; then
    echo "Failed to create tus upload:"
    printf '%s\n' "$CREATE_HEADERS"
    exit 1
fi
# Rewrite container/host-relative Location if needed for local access
case "$LOCATION" in
    http://localhost:8080/*|http://127.0.0.1:8080/*) ;;
    http://test-entry:8080/*)
        LOCATION="http://localhost:8080${LOCATION#http://test-entry:8080}"
        ;;
    /*)
        LOCATION="$PORTAL$LOCATION"
        ;;
esac
echo "Upload location: $LOCATION"

echo "Uploading file in ${CHUNK_SIZE}-byte chunks (this will take several minutes)..."
START_TIME=$(date +%s)
OFFSET=0
while [ "$OFFSET" -lt "$FILE_SIZE" ]; do
    COUNT=$CHUNK_SIZE
    REMAIN=$((FILE_SIZE - OFFSET))
    if [ "$REMAIN" -lt "$COUNT" ]; then
        COUNT=$REMAIN
    fi

    dd if="$MP4_FILE" iflag=skip_bytes,count_bytes skip="$OFFSET" count="$COUNT" status=none | \
    curl -sf -X PATCH "$LOCATION" \
        -H "Tus-Resumable: 1.0.0" \
        -H "Upload-Offset: $OFFSET" \
        -H "Content-Type: application/offset+octet-stream" \
        --data-binary @- \
        -o /dev/null

    OFFSET=$((OFFSET + COUNT))
    echo "Uploaded ${OFFSET}/${FILE_SIZE} bytes"
done
END_TIME=$(date +%s)
UPLOAD_DURATION=$((END_TIME - START_TIME))
echo "Upload completed in ${UPLOAD_DURATION}s"

echo ""
echo "--- Phase 5: Waiting for operation to complete ---"
START_TIME=$(date +%s)
TIMEOUT=3600
while true; do
    ELAPSED=$(( $(date +%s) - START_TIME ))
    if [ "$ELAPSED" -gt "$TIMEOUT" ]; then
        echo "TIMEOUT: Operation did not complete within ${TIMEOUT}s"
        STATUS=$(curl -sf "$PORTAL/updates/$OP_ID" | jq -r .status || echo "unknown")
        echo "Final status: $STATUS"
        exit 1
    fi

    STATUS=$(curl -sf "$PORTAL/updates/$OP_ID" | jq -r .status || echo "unknown")
    echo "Status: $STATUS (elapsed: ${ELAPSED}s)"

    case "$STATUS" in
        SUCCEEDED)
            echo "Operation succeeded!"
            break
            ;;
        FAILED|ROLLED_BACK|ROLLBACK_FAILED)
            echo "Operation finished unsuccessfully: $STATUS"
            curl -s "$PORTAL/updates/$OP_ID" || true
            docker logs test-worker 2>&1 | tail -80 || true
            exit 1
            ;;
        *)
            sleep 5
            ;;
    esac
done

echo ""
echo "--- Phase 6: Testing worker stop/restart ---"
echo "Stopping worker service..."
docker stop test-worker
echo "Worker stopped"
sleep 3

echo "Restarting worker service..."
docker start test-worker
echo "Worker restarted"
sleep 5

echo ""
echo "--- Phase 7: Verifying file integrity ---"
echo "Verifying uploaded file SHA256..."
go run test/verify/main.go "$MP4_FILE" "$EXPECTED_HASH"

echo ""
echo "============================================"
echo "  TEST PASSED"
echo "============================================"
echo "Operation ID: $OP_ID"
echo "Upload duration: ${UPLOAD_DURATION}s"
echo "File size: $(du -h "$MP4_FILE" | cut -f1)"
echo "SHA256: $EXPECTED_HASH"
