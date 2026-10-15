#!/usr/bin/env bash
# Scripted end-to-end: a mock OpenAI-compatible server drives a multi-step
# turn (list a directory, write summary.txt) through the xbot binary.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
mkdir -p "$ROOT/bin"

go build -o "$ROOT/bin/xbot" ./cmd/xbot
go build -o "$ROOT/bin/mockopenai" ./scripts/mockopenai

PORT=18765
WORKDIR=$(mktemp -d)
DATA=$(mktemp -d)
OUT=$(mktemp)
ERR=$(mktemp)
PID=""

cleanup() {
  status=$?
  if [[ "$status" -ne 0 ]]; then
    echo "--- logs ---" >&2
    cat "$ERR" >&2 || true
    echo "--- stdout ---" >&2
    cat "$OUT" >&2 || true
    echo "--- summary ---" >&2
    cat "$WORKDIR/summary.txt" >&2 || true
  fi
  if [[ -n "$PID" ]]; then
    kill "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  rm -rf "$WORKDIR" "$DATA" "$OUT" "$ERR"
}
trap cleanup EXIT

printf 'alpha\n' > "$WORKDIR/a.txt"
printf 'beta\n' > "$WORKDIR/b.txt"

"$ROOT/bin/mockopenai" -addr "127.0.0.1:$PORT" -dir "$WORKDIR" >"$ERR" 2>&1 &
PID=$!

ready=0
for _ in $(seq 1 50); do
  if (echo >/dev/tcp/127.0.0.1/"$PORT") 2>/dev/null; then
    ready=1
    break
  fi
  sleep 0.1
done
if [[ "$ready" -ne 1 ]]; then
  echo "mock server did not start" >&2
  cat "$ERR" >&2
  exit 1
fi

export XBOT_API_KEY=test
export XBOT_BASE_URL="http://127.0.0.1:$PORT/v1"
export XBOT_MODEL=mock

"$ROOT/bin/xbot" --root "$DATA" new demo --persona "You are a test bot."

set +e
(
  cd "$WORKDIR"
  printf '%s\n' 'List this directory and write a summary to summary.txt' 'exit' |
    "$ROOT/bin/xbot" --root "$DATA" chat demo --yes --max-steps 6
) >"$OUT" 2>>"$ERR"
status=$?
set -e

if [[ "$status" -ne 0 ]]; then
  echo "xbot chat failed: $status" >&2
  cat "$ERR" >&2
  echo "--- stdout ---" >&2
  cat "$OUT" >&2
  exit 1
fi

grep -q "Summary written" "$OUT"
grep -q "a.txt" "$WORKDIR/summary.txt"
grep -q "b.txt" "$WORKDIR/summary.txt"
echo "e2e ok"
