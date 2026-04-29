#!/usr/bin/env bash
# Builds the image, runs it, polls /healthz, exercises the calculate endpoint
# with a large-order edge case, tears down. Exits non-zero on any failure.

set -euo pipefail

IMAGE="${IMAGE:-pack-calculator:latest}"
NAME="pack-calculator-smoke"
HOST_PORT="${HOST_PORT:-18080}"

cleanup() {
  docker rm -f "$NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

cleanup
echo "==> starting container ($IMAGE on host port $HOST_PORT)"
docker run -d --rm --name "$NAME" -p "$HOST_PORT:8080" "$IMAGE" >/dev/null

echo "==> waiting for /healthz"
for i in $(seq 1 30); do
  if curl -fs -m 1 "http://127.0.0.1:$HOST_PORT/healthz" >/dev/null; then
    echo "    ready after ${i} attempt(s)"
    break
  fi
  sleep 0.5
  if [[ $i -eq 30 ]]; then
    echo "ERROR: container did not become healthy" >&2
    docker logs "$NAME" >&2 || true
    exit 1
  fi
done

echo "==> setting pack sizes to 23,31,53"
curl -fs -X PUT \
  -H 'Content-Type: application/json' \
  -d '{"pack_sizes":[23,31,53]}' \
  "http://127.0.0.1:$HOST_PORT/api/pack-sizes" | tee /tmp/smoke-set.json
echo

echo "==> calculating order=500000 (expect 9438 packs / 500000 items)"
RESULT=$(curl -fs -X POST \
  -H 'Content-Type: application/json' \
  -d '{"order":500000}' \
  "http://127.0.0.1:$HOST_PORT/api/calculate")
echo "    $RESULT"

echo "$RESULT" | grep -q '"shipped_items":500000' || { echo "ERROR: wrong shipped_items"; exit 1; }
echo "$RESULT" | grep -q '"total_packs":9438'      || { echo "ERROR: wrong total_packs"; exit 1; }

echo "==> OK: container reachable and edge case correct"
