#!/usr/bin/env bash
# =============================================================================
# scripts/wait-for-stack.sh
# Polls until all four Nebu services are healthy or a 120s timeout is reached.
#
# Usage:
#    scripts/wait-for-stack.sh
#
# Exits 0 on success, 1 on timeout.
#
# Story 8-11 — AC #3: used as before_script in the integration-test-k8s CI job
# so that tests only run after the full stack is ready (GitLab CI services:
# start all sidecars in parallel without depends_on ordering).
# =============================================================================

set -euo pipefail

TIMEOUT=120
INTERVAL=2

# ---------------------------------------------------------------------------
# Health check functions — one per service
# ---------------------------------------------------------------------------
check_postgres() {
    if command -v pg_isready >/dev/null 2>&1; then
        pg_isready -h postgres -U nebu -d nebu >/dev/null 2>&1
        return $?
    fi
    # Fallback: TCP probe via wget/curl
    wget -q --spider tcp://postgres:5432
}

check_dex() {
    wget -q -O- 'http://dex:5556/dex/.well-known/openid-configuration' >/dev/null 2>&1
}

check_core() {
    wget -q -O- 'http://core:4000/health' >/dev/null 2>&1
}

check_gateway() {
    wget -q -O- 'http://gateway:8008/health' >/dev/null 2>&1
}

# ---------------------------------------------------------------------------
# Main polling loop
# ---------------------------------------------------------------------------
echo "Waiting for stack to become healthy (timeout: ${TIMEOUT}s)..."

elapsed=0
while [ "$elapsed" -lt "$TIMEOUT" ]; do
    all_ok=true

    # postgres
    if check_postgres; then
        echo "[OK]   postgres"
    else
        echo "[..]   postgres"
        all_ok=false
    fi

    # dex
    if check_dex; then
        echo "[OK]   dex"
    else
        echo "[..]   dex"
        all_ok=false
    fi

    # core
    if check_core; then
        echo "[OK]   core"
    else
        echo "[..]   core"
        all_ok=false
    fi

    # gateway
    if check_gateway; then
        echo "[OK]   gateway"
    else
        echo "[..]   gateway"
        all_ok=false
    fi

    if [ "$all_ok" = true ]; then
        echo "All services healthy after ${elapsed}s."
        exit 0
    fi

    sleep "$INTERVAL"
    elapsed=$((elapsed + INTERVAL))
done

echo "ERROR: stack not ready after ${TIMEOUT}s — timed out." >&2
exit 1
