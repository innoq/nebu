# Nebu Silber-Tier Load Test

## What is Silber-Tier?

**Silber-Tier** is Nebu's first performance quality gate: 500 concurrent users
sustaining real-time chat traffic on a 2x AWS EC2 m5.large infrastructure
(8 vCPU / 32 GB RAM total), with **no Redis, no NATS, no Kafka** — only
Elixir/OTP (ETS + pg Process Groups) and PostgreSQL.

Reference topology (from `docs/architecture/adr/`):

```
10 active rooms × 50 members = 500 concurrent users

Traffic mix:
  60%  GET /sync         (long-poll, always-active connections)
  20%  PUT /rooms/send   (message dispatch)
  10%  Presence + Typing indicators
   5%  CreateRoom / JoinRoom
   5%  Profile + misc

Infrastructure:
  2× AWS EC2 m5.large
  No Redis, NATS, Kafka — Elixir/OTP + PostgreSQL only
```

The MVP load test implements a simplified subset: one `send_event` + one
`/sync` per VU per iteration. A full traffic-mix test is out of scope for
this story.

---

## How to Run

**Prerequisite:** The full dev stack must be healthy. `make test-load-silber`
starts it automatically if needed.

```bash
make test-load-silber
```

This command:
1. Runs `docker compose up -d --wait` to ensure the stack is ready.
2. Starts the k6 container (`grafana/k6:0.50.0`) joined to `nebu_default`.
3. Mounts `tests/load/` into the container and runs `k6_chat.js`.
4. Leaves the stack running (no `docker compose down`).

**Override target URL or Dex URL:**

```bash
NEBU_LOAD_TARGET_URL=http://my-host:8008 NEBU_DEX_URL=http://my-dex:5556 make test-load-silber
```

**Dry-run (verify Docker command without running):**

```bash
make -n test-load-silber
```

---

## Threshold Definitions

| Threshold | Value | Meaning |
|-----------|-------|---------|
| `http_req_duration{name:send_event}` p95 | < 200 ms | Matrix PUT send must be acknowledged within 200 ms at the 95th percentile |
| `http_req_duration{name:sync}` p95 | < 500 ms | Matrix GET /sync must return within 500 ms at the 95th percentile |
| `http_req_failed` rate | < 0.1% (0.001) | Network-level failures (connection errors, timeouts) must stay below 0.1% |
| `checks` rate | > 99% (0.99) | HTTP-level checks (status code assertions) must pass at least 99% of the time |

---

## Interpreting k6 Output

k6 prints a summary table at the end of the run. Each threshold shows as
either a green checkmark (pass) or a red cross (fail):

```
✓ http_req_duration{name:send_event}  p(95)<200   avg=84ms  p(95)=143ms
✓ http_req_duration{name:sync}        p(95)<500   avg=210ms p(95)=380ms
✓ http_req_failed                     rate<0.001  0.00% ✓ 0 ✗ 0
```

A failed threshold looks like:

```
✗ http_req_duration{name:send_event}  p(95)<200   avg=180ms p(95)=247ms
```

**Exit codes:**

| Code | Meaning |
|------|---------|
| `0`  | All thresholds passed — Silber-Tier validated |
| `99` | One or more thresholds violated — performance regression detected |

A non-zero exit causes `make test-load-silber` to return an error, making
the test suitable as an optional pre-release gate in CI.

---

## Test Structure

```
tests/load/
  k6_chat.js   — k6 load test script (Goja JS runtime, NOT Node.js)
  README.md    — this file
```

The k6 script uses three k6 lifecycle hooks:

| Hook | Runs | Purpose |
|------|------|---------|
| `setup()` | Once, before VU ramp-up | Dex Authorization Code flow → Matrix access token |
| `default(data)` | Every iteration per VU | createRoom (first iter), send_event, sync, sleep(1) |
| `teardown(data)` | Once, after VU ramp-down | (not used — stack remains running) |

`setup()` returns a single shared `access_token`. All 500 VUs share the same
token (kai / instance_admin). This is intentional for throughput measurement:
we test server-side capacity, not per-user isolation.

---

## Important k6 Runtime Notes

k6 uses the **Goja JS runtime** (ES2015+, NOT Node.js):

- Use `__ENV.VAR_NAME` — NOT `process.env.VAR_NAME`
- Use `import http from 'k6/http'` — NOT `require('http')`
- Use `sleep(n)` from `'k6'` — NOT `setTimeout`
- No npm, no node_modules, no CommonJS `require()`

The test script is a single self-contained file. No package.json needed.
