#!/usr/bin/env bash
set -euo pipefail
image=${1:-pageweave:local}
name="pageweave-smoke-${RANDOM}"
cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT
cid=$(docker run -d --name "$name" --init --shm-size=256m --security-opt seccomp=deploy/chromium-seccomp.json -p 127.0.0.1::7779 "$image")
port=$(docker port "$cid" 7779/tcp | awk -F: '{print $NF}')
ready=false
for attempt in $(seq 1 40); do
  if docker exec "$cid" pageweave healthcheck; then ready=true; break; fi
  if [ "$(docker inspect -f '{{.State.Running}}' "$cid")" != true ]; then docker logs "$cid"; exit 1; fi
  sleep 0.5
done
if [ "$ready" != true ]; then docker logs "$cid"; exit 1; fi
test "$(docker exec "$cid" id -u)" = 10001
docker exec "$cid" chromium --version
docker exec "$cid" pageweave --version
# No --no-sandbox may be present in the browser command line.
if docker exec "$cid" sh -c 'cat /proc/[0-9]*/cmdline 2>/dev/null' | tr '\0' '\n' | grep -x -- '--no-sandbox'; then exit 1; fi
base="http://127.0.0.1:${port}"
test "$(curl -s -o /tmp/pageweave-smoke-response -w '%{http_code}' "$base/health/live")" = 200
test "$(curl -s -o /tmp/pageweave-smoke-response -w '%{http_code}' -H 'Content-Type: application/json' -d '{}' "$base/extract")" = 422
test "$(curl -s -o /tmp/pageweave-smoke-response -w '%{http_code}' -H 'Content-Type: application/json' -d '{"url":"http://127.0.0.1/"}' "$base/extract")" = 403
# SIGTERM must finish without Docker's SIGKILL fallback.
docker stop -t 20 "$cid" >/dev/null
test "$(docker inspect -f '{{.State.ExitCode}}' "$cid")" = 0
docker logs "$cid"
