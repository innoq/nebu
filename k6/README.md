# Nebu Load Tests (k6)

k6 load test scenarios for Nebu — Silver Tier (500 VU) and Gold Tier (1000 VU).

Story 13-5 AC5.

---

## Prerequisites

### Option A — Docker-based k6 (recommended, no local install)

Docker must be installed and running. All `make` targets use the official
`grafana/k6` Docker image automatically.

### Option B — Local k6 install

```bash
# macOS
brew install k6

# Linux
sudo gpg -k
sudo gpg --no-default-keyring \
  --keyring /usr/share/keyrings/k6-archive-keyring.gpg \
  --keyserver hkp://keyserver.ubuntu.com:80 \
  --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" \
  | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update && sudo apt-get install k6
```

---

## Test Setup — Multi-Gateway Stack

Story 13-5 AC3: start 2 gateway instances sharing the Core at `core:9000`.

```bash
# First-time setup (generates .secrets/internal_secret)
make setup

# Start full stack with 2 gateway replicas
docker compose -f docker-compose.yml -f docker-compose.scale.yml up \
  --scale gateway=2 --build -d --wait
```

Verify both gateways registered with Core:

```bash
docker compose logs core | grep "node registered"
```

Both instances use the same PSK secret — Core logs will show two distinct
node registrations.

---

## Running Silver Tier (500 VU)

```bash
# Via Docker (no local k6 needed)
docker run --rm -v $(pwd)/k6:/scripts \
  --network=nebu_default \
  -e BASE_URL=http://gateway:8008 \
  grafana/k6:0.55.0 run \
    -e TEST_USER=kai \
    -e TEST_PASSWORD=changeme \
    "-e TEST_ROOM_ID=!your-room-id:localhost" \
  /scripts/scenarios/silver-tier.js

# Via local k6
k6 run k6/scenarios/silver-tier.js \
  -e BASE_URL=http://localhost:8008 \
  -e TEST_USER=kai \
  -e TEST_PASSWORD=changeme \
  -e TEST_ROOM_ID='!your-room-id:localhost'
```

---

## Running Gold Tier (1000 VU)

```bash
# Via Docker (no local k6 needed)
docker run --rm -v $(pwd)/k6:/scripts \
  --network=nebu_default \
  -e BASE_URL=http://gateway:8008 \
  grafana/k6:0.55.0 run \
    -e TEST_USER=kai \
    -e TEST_PASSWORD=changeme \
    "-e TEST_ROOM_ID=!your-room-id:localhost" \
  /scripts/scenarios/gold-tier.js

# Via local k6
k6 run k6/scenarios/gold-tier.js \
  -e BASE_URL=http://localhost:8008 \
  -e TEST_USER=kai \
  -e TEST_PASSWORD=changeme \
  -e TEST_ROOM_ID='!your-room-id:localhost'
```

---

## Expected Results

| Metric                         | Silver Tier (500 VU) | Gold Tier (1000 VU) |
|-------------------------------|---------------------|---------------------|
| Send p95 latency               | < 800 ms            | < 500 ms            |
| Login p95 latency              | < 1200 ms           | < 1000 ms           |
| Sync p95 latency               | < 1000 ms           | < 800 ms            |
| Error rate (`http_req_failed`) | < 1%                | < 1%                |
| Duration                       | 5 min               | 5 min               |

Gold Tier thresholds are stricter because the 2-gateway setup is expected to
provide better throughput per VU under equivalent load.

---

## Running Against AWS / Stackit Production Deployments

Change `BASE_URL` to the ALB DNS name:

```bash
# AWS (ECS Fargate + ALB from ADR-014 Story 13-2c)
k6 run k6/scenarios/gold-tier.js \
  -e BASE_URL=https://nebu.your-alb.aws.example.com \
  -e TEST_USER=your-matrix-user \
  -e TEST_PASSWORD=your-password \
  -e TEST_ROOM_ID='!room-id:your-server-name'

# Stackit (VM + ALB from ADR-014 Story 13-3a)
k6 run k6/scenarios/gold-tier.js \
  -e BASE_URL=https://nebu.your-stackit-alb.example.com \
  -e TEST_USER=your-matrix-user \
  -e TEST_PASSWORD=your-password \
  -e TEST_ROOM_ID='!room-id:your-server-name'
```

For Kubernetes deployments (Story 13-4c), point `BASE_URL` at the Ingress
hostname configured in `deploy/helm/nebu/values.yaml`.

---

## Syntax Validation (CI gate)

```bash
make test-load-syntax
```

Runs `k6 inspect` on both scenario files (Docker-based, no running stack
needed) and validates `docker-compose.scale.yml` with
`docker compose config --quiet`.

This gate runs in CI as part of the `validate-iac` job.

---

## Metrics Reported (Story 13-5 AC2)

Both scenarios report the following custom metrics in the k6 summary:

| Metric                | Description                              |
|----------------------|------------------------------------------|
| `nebu_login_duration` | p50 / p95 / p99 login latency            |
| `nebu_sync_duration`  | p50 / p95 / p99 sync latency             |
| `nebu_send_duration`  | p50 / p95 / p99 send latency             |
| `nebu_login_errors`   | Error rate for login endpoint            |
| `nebu_sync_errors`    | Error rate for sync endpoint             |
| `nebu_send_errors`    | Error rate for send endpoint             |
| `nebu_total_requests` | Total requests issued (login+sync+send)  |
| `http_req_failed`     | Built-in k6 overall HTTP failure rate    |
