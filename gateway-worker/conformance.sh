#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root/gateway-worker"
# A dedicated process group lets cleanup include cf's Vite/workerd children.
rm -rf .cloudflare/state
# Fresh local state each run: the official gateway caps new installs per IP per day.
setsid npx cf dev > conformance-dev.log 2>&1 &
pid=$!
# The cf dev log is kept when the run fails, to diagnose rare local failures.
cleanup() {
  local status=$?
  kill -- "-$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true
  if [[ $status == 0 ]]; then rm -f conformance-dev.log; else echo "cf dev log kept: gateway-worker/conformance-dev.log" >&2; fi
}
trap cleanup EXIT
ready=0
for ((i=0; i<120; i++)); do
  if curl --fail --silent http://127.0.0.1:8787/healthz >/dev/null; then ready=1; break; fi
  kill -0 "$pid" 2>/dev/null || { cat conformance-dev.log; exit 1; }
  sleep 0.5
done
[[ "$ready" == 1 ]] || { cat conformance-dev.log; exit 1; }
cd "$root"
XBOT_GATEWAY_URL=http://127.0.0.1:8787 go test -count=1 -run TestConformanceURL ./internal/gateway
