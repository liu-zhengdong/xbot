# XBot official gateway

Signed mailbox relay for the Go `internal/gateway` HTTP protocol, backed by
SQLite Durable Objects. Mailbox objects own queues and hibernating WebSocket
waiters; Directory owns registrations and global byte accounting.

Local development: `npm ci` then `npx cf dev` in this directory (127.0.0.1:8787).
Type check: `npx tsc --noEmit` in this directory.
Run `./gateway-worker/conformance.sh` from any directory after the Go conformance
suite is merged. For an already running server, from the repository root run:
`XBOT_GATEWAY_URL=http://127.0.0.1:8787 go test -count=1 -run TestConformanceURL ./internal/gateway`.
No deployment is performed by these commands.

Registration is open unless the optional `REGISTER_TOKEN` Worker secret is set.
Never put its value in source control. Existing registrations need no token.
`MAX_INSTALLS` defaults to 1000 (the Go self-hosted default is 64).
Official gateway deviation from the Go protocol: new installs are limited to 30
per client IP per UTC day; re-registrations are exempt. Only a daily salted SHA-256
prefix and count are stored, never the raw IP. Live conformance runs should set
`XBOT_GATEWAY_TEST_SEED` so reruns reuse installs.
Idle, empty installations become eligible for eviction after 30 days.

Limits match Go: 64 KiB request and normalized envelope; pair 64 envelopes/1 MiB;
recipient 512/8 MiB; sender 16 MiB across recipients; gateway 256 MiB.
Envelope TTL is 7 days, acknowledgement tombstones last 10 minutes.
Poll returns at most 32 envelopes/768 KiB; wait defaults to 25 seconds and clamps
to 0–30 seconds. Authentication tolerates 300 seconds of clock skew.
Ack accepts at most 64 lowercase hexadecimal ids. Expiry alarms free quota even
when a mailbox receives no traffic. Only the latest poll per install waits.

**Free-plan budget (idle estimate):** Workers Free allows 100k requests/day; Durable Objects allow
100k requests/day and 100k SQLite rows written/day. Each always-on install ≈ 3,456
polls/day = 3,456 Worker requests + 3,456 DO requests (+ a websocket close per wait),
so the Free plan carries roughly 25 idle always-on installs; sends cost 3 DO requests
each. The hourly `seen` write per install, sends, acks, and registrations also
consume the 100k rows-written/day allowance. Past that, move to the Workers Paid plan; do not enable it from code.

G7 short-code support plugs in as a separate `Codes` Durable Object and
`/v1/code*` routes; it is not implemented here.

Offline wire tests: `node --experimental-strip-types --test gateway-worker/src/wire.test.mjs`
(from the repository root). Runtime conformance and latency require local sockets.
