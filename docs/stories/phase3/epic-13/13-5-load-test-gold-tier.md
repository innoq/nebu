---
status: ready-for-dev
epic: 13
story: 5
security_review: not-needed
matrix: false
ui: false
---

# Story 13.5: Load Test — Gold Tier (1000 Concurrent Users, Multi-Gateway)

Status: ready-for-dev

## Story

As a system operator,
I want a k6 load test scenario that validates Nebu under Gold Tier load (1000 concurrent users across 2 gateway instances),
So that I can verify horizontal scalability before production deployment.

**Size:** M

---

## Acceptance Criteria

**AC1 — k6 scenario file exists and runs:**
Given `k6/scenarios/gold-tier.js` exists,
When `k6 run k6/scenarios/gold-tier.js --vus 1000 --duration 5m` runs against a 2-gateway stack,
Then p95 latency for `PUT /send` is < 500 ms and error rate is < 1%

**AC2 — k6 summary reports required metrics:**
Given the load test results,
When the k6 summary is inspected,
Then the following are reported: p50/p95/p99 latency for sync, send, and login; total requests/sec; error rate per endpoint

**AC3 — 2-gateway Docker Compose override:**
Given `docker-compose.scale.yml` at the project root,
When `docker compose -f docker-compose.yml -f docker-compose.scale.yml up --scale gateway=2` runs,
Then both gateway instances register with Core via PSK and share load (both appear in Core node registry logs)

**AC4 — Silver Tier scenario also included:**
Given `k6/scenarios/silver-tier.js` exists,
When `k6 run k6/scenarios/silver-tier.js --vus 500 --duration 5m` runs,
Then exit code 0 (scenario completes without script errors)

**AC5 — k6/README.md documents load test setup:**
Given `k6/README.md`,
When it is read,
Then it documents: test setup requirements (Docker Compose scale, k6 installation), expected results for Silver (500 VU) and Gold (1000 VU) tiers, and how to run against AWS/Stackit deployments

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. `k6/scenarios/gold-tier.js` script validation — Syntax test**
- Given: `k6/scenarios/gold-tier.js` does NOT exist
- When: `k6 run k6/scenarios/gold-tier.js --no-vus-by-metric` (or any k6 syntax check) runs
- Then: exit non-zero (file not found)
- [Passes after implementation: `k6 inspect k6/scenarios/gold-tier.js` exits 0]

**2. `docker-compose.scale.yml` exists and is valid**
- Given: `docker-compose.scale.yml` does NOT exist
- When: `docker compose -f docker-compose.yml -f docker-compose.scale.yml config` runs
- Then: exit non-zero
- [Passes after implementation: config validates successfully]

**3. k6 scenario structure correctness**
- Given: `k6/scenarios/gold-tier.js`
- When: `k6 inspect k6/scenarios/gold-tier.js` runs
- Then: exit 0 with a valid scenario definition showing `vus` and `duration` options

Note: Full load test against a running stack is a manual/Tier-2 test per ADR-014; the automated gate is the k6 syntax check and Docker Compose config validation.
