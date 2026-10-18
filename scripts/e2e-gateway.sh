#!/usr/bin/env bash
# Two installs exchange signed bot messages using only the scripted model.
# XBOT_E2E_OFFICIAL=1 leaves the gateway unset so both use the official default; the outage step is skipped.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK="$ROOT/bin/e2e-gateway"
cd "$ROOT"
rm -rf "$WORK"
mkdir -p "$WORK/build-tmp"
export TMPDIR="$WORK/build-tmp"
PIDS=()
cleanup() {
  status=$?
  if [[ "$status" -ne 0 ]]; then
    for f in "$WORK"/*.log; do
      [[ -f "$f" ]] || continue
      echo "--- $(basename "$f") ---" >&2
      grep -vi token "$f" >&2 || true
    done
  fi
  for pid in "${PIDS[@]}"; do kill "$pid" 2>/dev/null || true; done
  for pid in "${PIDS[@]}"; do wait "$pid" 2>/dev/null || true; done
}
trap cleanup EXIT
ms() { python3 -c 'import time; print(time.monotonic_ns()//1000000)'; }
started=$(ms)
go build -o "$ROOT/bin/xbot" ./cmd/xbot
go build -o "$ROOT/bin/mockopenai" ./scripts/mockopenai
MOCK=${XBOT_E2E_MOCK_PORT:-18865}
GW=${XBOT_E2E_GATEWAY_PORT:-18866}
P1=${XBOT_E2E_A_PORT:-18867}
P2=${XBOT_E2E_B_PORT:-18868}
OFFICIAL=${XBOT_E2E_OFFICIAL:-}
export XBOT_GATEWAY_TOKEN=
[[ -n "$OFFICIAL" ]] || XBOT_GATEWAY_TOKEN=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
export XBOT_API_KEY=test XBOT_BASE_URL="http://127.0.0.1:$MOCK/v1" XBOT_MODEL=mock XBOT_AUTO_UPGRADE=0
unset XBOT_VAULT_SOCKET
"$ROOT/bin/mockopenai" -addr "127.0.0.1:$MOCK" >"$WORK/mock.log" 2>&1 &
PIDS+=("$!")
if [[ -z "$OFFICIAL" ]]; then
  "$ROOT/bin/xbot" gateway serve --addr "127.0.0.1:$GW" --dir "$WORK/gw" >"$WORK/gateway.log" 2>&1 &
  GW_PID=$!
  PIDS+=("$GW_PID")
fi
wait_http() {
  for _ in $(seq 1 50); do
    if curl -fsS "$1" >/dev/null 2>&1; then return; fi
    sleep 0.1
  done
  echo "server did not start: $1" >&2
  return 1
}
wait_http "http://127.0.0.1:$MOCK/health"
[[ -n "$OFFICIAL" ]] || wait_http "http://127.0.0.1:$GW/healthz"
X="$ROOT/bin/xbot"
A="$WORK/a"
B="$WORK/b"
"$X" --root "$A" new ada >"$WORK/setup.log" 2>&1
"$X" --root "$B" new bob >>"$WORK/setup.log" 2>&1
[[ -n "$OFFICIAL" ]] || "$X" --root "$A" remote gateway "http://127.0.0.1:$GW"
code=$("$X" --root "$A" remote invite | head -1)
"$X" --root "$A" serve --http "127.0.0.1:$P1" --interval 1s --no-auto-upgrade >"$WORK/a.log" 2>&1 &
PIDS+=("$!")
XBOT_GATEWAY_TOKEN= "$X" --root "$B" remote join "$code" --name a --as b >>"$WORK/setup.log"
dir_started=$(ms)
XBOT_GATEWAY_TOKEN= "$X" --root "$B" serve --http "127.0.0.1:$P2" --interval 1s --no-auto-upgrade >"$WORK/b.log" 2>&1 &
PIDS+=("$!")
for log in a b; do
  for _ in $(seq 1 100); do
    if grep -q 'xbot: remote: registered' "$WORK/$log.log"; then break; fi
    sleep 0.1
  done
  grep -q 'xbot: remote: registered' "$WORK/$log.log"
done
for _ in $(seq 1 100); do
  if "$X" --root "$A" remote list | grep -q "^b$(printf '\t')"; then break; fi
  sleep 0.1
done
"$X" --root "$A" remote list | grep -q "^b$(printf '\t')" || { echo "FAIL: invite pairing did not complete" >&2; exit 1; }
for _ in $(seq 1 700); do
  if grep -rqs '"bob"' "$A/remote/dirs/"; then break; fi
  sleep 0.1
done
grep -rqs '"bob"' "$A/remote/dirs/" || { echo "FAIL: no gateway dir within 70s" >&2; exit 1; }
echo "PASS gateway dir=$(( $(ms)-dir_started ))ms"
sent=$(ms)
printf 'REMOTE_PING\nexit\n' | "$X" --root "$A" chat ada --max-steps 4 >"$WORK/chat.log" 2>&1
for _ in $(seq 1 600); do
  if grep -rqs "got remote reply" "$A/bots/ada/transcript" && grep -rqs "bot:ada@a" "$B/bots/bob/transcript"; then
    ended=$(ms)
    echo "PASS gateway round-trip=$((ended-sent))ms total=$((ended-started))ms"
    break
  fi
  sleep 0.1
done
grep -rqs "got remote reply" "$A/bots/ada/transcript" || { echo "FAIL: no remote reply within 60s" >&2; exit 1; }
[[ -z "$OFFICIAL" ]] || { "$X" --root "$A" remote gateway; "$X" --root "$B" remote gateway; exit 0; }
kill "$GW_PID"
wait "$GW_PID" || true
sent=$(ms)
printf 'REMOTE_PING_RETRY\nexit\n' | "$X" --root "$A" chat ada --max-steps 4 >"$WORK/retry-chat.log" 2>&1
compgen -G "$A/remote/outbox/*.json" >/dev/null || { echo "FAIL: outage message not queued" >&2; exit 1; }
"$X" gateway serve --addr "127.0.0.1:$GW" --dir "$WORK/gw" >>"$WORK/gateway.log" 2>&1 &
PIDS+=("$!")
wait_http "http://127.0.0.1:$GW/healthz"
for _ in $(seq 1 400); do
  if grep -rqs "REMOTE_OUTAGE_DELIVERY" "$B/bots/bob/transcript"; then
    ended=$(ms)
    echo "PASS gateway outage-retry=$((ended-sent))ms"
    exit 0
  fi
  sleep 0.1
done
echo "FAIL: outage message not delivered within 40s" >&2
exit 1
