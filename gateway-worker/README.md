# XBot official gateway

Signed mailbox relay for the Go `internal/gateway` HTTP protocol, backed by
SQLite Durable Objects. Mailbox objects own queues and hibernating WebSocket
waiters; Directory owns registrations and global byte accounting.
Deployed with `cf deploy` to https://xbot.babelgo.cn (see ADR 0011).

Requires Node ≥ 22.18. In this directory: `npm ci`, then
- `npm test`: unit tests (`src/wire.test.mjs`, `src/objects.test.mjs`)
- `npm run check`: type check
- `npm run dev`: local server on 127.0.0.1:8787
- `npm run conformance`: fresh local state, `cf dev`, then the Go conformance suite against it

For an already running server, from the repository root run:
`XBOT_GATEWAY_URL=http://127.0.0.1:8787 go test -count=1 -run TestConformanceURL ./internal/gateway`.
Live runs should set `XBOT_GATEWAY_TEST_SEED` so reruns reuse installs.

Registration is open unless the optional `REGISTER_TOKEN` Worker secret is set.
Never put its value in source control. Existing registrations need no token.
`MAX_INSTALLS` defaults to 1000 (Go's limit is a fixed 64).
Deliberate deviations from the Go server: open registration; at most 30 new installs
per client IP per UTC day (429), re-registrations exempt; the IP is counted under an
HMAC with a random key that exists only for that day and is purged with its counts
by a daily alarm; install liveness (`seen`) is written at most hourly.
Idle, empty installations become eligible for eviction after 30 days.

Limits match Go: 64 KiB request and normalized envelope; pair 64 envelopes/1 MiB;
recipient 512/8 MiB; sender 16 MiB across recipients; gateway 256 MiB.
Envelope TTL is 7 days, acknowledgement tombstones last 10 minutes.
Poll returns at most 32 envelopes/768 KiB; wait defaults to 25 seconds and clamps
to 0–30 seconds. Authentication tolerates 300 seconds of clock skew.
Ack accepts at most 64 lowercase hexadecimal ids. Expiry alarms free quota even
when a mailbox receives no traffic. Only the latest poll per install waits.

**Free-plan budget:** Workers Free allows 100k requests/day; Durable Objects allow
100k requests/day and 100k SQLite rows written/day. An empty poll is 1 Worker + 1 DO
request; a send is 3 DO requests, an ack 2. A paired always-on install makes about
3,456 polls/day plus, per peer, ~96 directory pushes and their acks: roughly
3,750 Worker requests, 4,000 DO requests and 700 rows written per day. The Free plan
therefore carries about 20–25 such installs, fewer with several peers. Unpaired
clients do not contact the gateway. Unauthenticated requests still count, so abuse
can exhaust the daily quota; add WAF/rate-limit rules if that happens. Past the
limits, move to the Workers Paid plan; do not enable it from code.

G7 short codes and pending installs (`/v1/code`, `/v1/code/cancel`,
`/v1/code/claim`, `/v1/code/admit`) live in Directory: lookups are stored only as
SHA-256, a record answers 410 for an hour after expiry or cancel and 404 after that,
five claimers use up a code, and failed lookups are limited per IP (IPv6 by /64)
and globally per minute. A pending install's Mailbox carries its owner and expiry;
its alarm drops its queue at expiry. Deviation: mail the pending install already
sent to an owner stays in that owner's queue when it expires or moves to another
owner (Go drops it), so owners must ignore senders they do not know.
