#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# ///
"""
ci_gate.py — Run the full Nebu CI gate and return a structured JSON report.

Orchestrates: build → unit-go → unit-elixir → env-reset → e2e → integration → teardown.
JSON report goes to stdout. Verbose step output goes to stderr.

Usage:
  python3 ci_gate.py [options]

Options:
  --story ID        Story ID to include in the report
  --only SUITE      Run only one suite: build | unit-go | unit-elixir | e2e | integration
  --skip SUITE      Skip a suite (repeatable)
  --env-timeout N   Seconds to wait for environment startup (default: 120)
  --no-reset        Skip docker compose down --volumes before startup
  --env-up          Start environment only (no tests), then exit
  --env-down        Tear down environment only, then exit
"""

import argparse
import json
import re
import subprocess
import sys
import time
import urllib.request
from datetime import datetime, timezone


# ── helpers ──────────────────────────────────────────────────────────────────

def log(msg):
    print(f"[ci-gate] {msg}", file=sys.stderr, flush=True)


def run(cmd, timeout=300, cwd=None):
    """Run a shell command, return (returncode, stdout+stderr combined)."""
    log(f"$ {cmd}")
    result = subprocess.run(
        cmd, shell=True, capture_output=True, text=True, timeout=timeout, cwd=cwd
    )
    output = result.stdout + result.stderr
    if output.strip():
        print(output, file=sys.stderr, flush=True)
    return result.returncode, output


def suite_result(status, failures=None, errors=None, passed=None, failed=None, **extra):
    r = {"status": status}
    if errors is not None:
        r["errors"] = errors
    if failures is not None:
        r["failures"] = failures
    if passed is not None or failed is not None:
        r["count"] = {"passed": passed or 0, "failed": failed or 0}
    r.update(extra)
    return r


# ── output parsers ────────────────────────────────────────────────────────────

def parse_go(output):
    failures = re.findall(r"--- FAIL: (\S+)", output)
    passed_m = re.search(r"ok\s+\S+\s+[\d.]+s", output)
    failed_m = re.search(r"FAIL\s+\S+", output)
    # count from "=== RUN" lines as proxy
    run_count = len(re.findall(r"^=== RUN", output, re.MULTILINE))
    failed_count = len(failures)
    passed_count = max(0, run_count - failed_count) if run_count else None
    return failures, passed_count, failed_count


def parse_elixir(output):
    failures = re.findall(r"^\s+\d+\) (.+)$", output, re.MULTILINE)
    m = re.search(r"(\d+) tests?, (\d+) failures?", output)
    if m:
        total, failed = int(m.group(1)), int(m.group(2))
        return failures, total - failed, failed
    return failures, None, len(failures)


def parse_playwright(output):
    # Playwright list reporter: "  N passed", "  N failed"
    failures = re.findall(r"✘\s+(.+?)(?:\s+\(\d+ms\))?$", output, re.MULTILINE)
    if not failures:
        failures = re.findall(r"FAILED\s+(.+)", output)
    passed_m = re.search(r"(\d+) passed", output)
    failed_m = re.search(r"(\d+) failed", output)
    passed = int(passed_m.group(1)) if passed_m else None
    failed = int(failed_m.group(1)) if failed_m else len(failures)
    return failures, passed, failed


def parse_godog(output):
    failures = re.findall(r"--- FAIL: (\S+)", output)
    # Godog scenario summary: "N scenarios (N passed, N failed)"
    m = re.search(r"(\d+) scenarios? \((.+?)\)", output)
    passed, failed = None, len(failures)
    if m:
        detail = m.group(2)
        pm = re.search(r"(\d+) passed", detail)
        fm = re.search(r"(\d+) failed", detail)
        passed = int(pm.group(1)) if pm else None
        failed = int(fm.group(1)) if fm else len(failures)
    return failures, passed, failed


# ── environment ───────────────────────────────────────────────────────────────

def env_reset(no_reset, timeout):
    if not no_reset:
        log("Tearing down existing environment (--volumes)...")
        run("docker compose down --volumes --remove-orphans", timeout=60)

    log("Starting environment...")
    rc, out = run("docker compose up -d --wait", timeout=120)
    if rc != 0:
        # --wait not supported on older compose; fall back
        rc, out = run("docker compose up -d", timeout=120)

    if rc != 0:
        return False, out

    return wait_healthy(timeout), out


def wait_healthy(timeout):
    checks = [
        ("Gateway", "http://localhost:8008/_matrix/client/versions"),
        ("Keycloak", "http://localhost:8080/"),
    ]
    deadline = time.time() + timeout
    log(f"Waiting up to {timeout}s for services to be healthy...")
    while time.time() < deadline:
        results = []
        for name, url in checks:
            try:
                r = urllib.request.urlopen(url, timeout=4)
                results.append(r.status < 500)
            except Exception:
                results.append(False)
        if all(results):
            log("All services healthy.")
            return True
        time.sleep(5)
    log(f"Timeout: services not healthy after {timeout}s.")
    return False


def teardown():
    log("Tearing down environment...")
    run("docker compose down", timeout=60)


# ── suites ────────────────────────────────────────────────────────────────────

def run_build():
    rc, out = run("make build-gateway && make build-core", timeout=300)
    if rc != 0:
        errors = [l.strip() for l in out.splitlines() if "error" in l.lower()][:20]
        return suite_result("fail", errors=errors or ["Build failed — see stderr"])
    return suite_result("pass", errors=[])


def run_unit_go():
    rc, out = run("make test-unit-go", timeout=300)
    failures, passed, failed = parse_go(out)
    status = "pass" if rc == 0 else "fail"
    return suite_result(status, failures=failures, passed=passed, failed=failed)


def run_unit_elixir():
    rc, out = run("make test-unit-elixir", timeout=300)
    failures, passed, failed = parse_elixir(out)
    status = "pass" if rc == 0 else "fail"
    return suite_result(status, failures=failures, passed=passed, failed=failed)


def run_e2e():
    rc, out = run("make test-e2e", timeout=300, cwd=None)
    failures, passed, failed = parse_playwright(out)
    status = "pass" if rc == 0 else "fail"
    return suite_result(
        status,
        failures=failures,
        passed=passed,
        failed=failed,
        missing_gherkin_feature=False,
        missing_coverage=False,
        missing_matrix_e2e_coverage=False,
    )


def run_integration():
    rc, out = run("make test-integration", timeout=300)
    failures, passed, failed = parse_godog(out)
    status = "pass" if rc == 0 else "fail"
    return suite_result(status, failures=failures, passed=passed, failed=failed)


# ── main ──────────────────────────────────────────────────────────────────────

SUITES = ["build", "unit-go", "unit-elixir", "e2e", "integration"]


def main():
    parser = argparse.ArgumentParser(description="Nebu CI gate runner.")
    parser.add_argument("--story", default=None)
    parser.add_argument("--only", choices=SUITES, default=None)
    parser.add_argument("--skip", action="append", default=[], metavar="SUITE")
    parser.add_argument("--env-timeout", type=int, default=120)
    parser.add_argument("--no-reset", action="store_true")
    parser.add_argument("--env-up", action="store_true", help="Start environment only, no tests")
    parser.add_argument("--env-down", action="store_true", help="Tear down environment only")
    args = parser.parse_args()

    # Environment-only shortcuts
    if args.env_down:
        teardown()
        print(json.dumps({"status": "pass", "action": "env-down"}))
        sys.exit(0)

    if args.env_up:
        healthy, _ = env_reset(args.no_reset, args.env_timeout)
        status = "pass" if healthy else "fail"
        print(json.dumps({"status": status, "action": "env-up"}))
        sys.exit(0 if healthy else 1)

    active = [args.only] if args.only else [s for s in SUITES if s not in args.skip]
    needs_env = any(s in active for s in ("e2e", "integration"))

    report = {
        "status": "pass",
        "story": args.story,
        "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%MZ"),
        "suites": {},
        "blocking_failures": [],
        "known_flaky": [],
    }

    try:
        # Build — critical: stop on failure
        if "build" in active:
            log("=== BUILD ===")
            result = run_build()
            report["suites"]["build"] = result
            if result["status"] == "fail":
                report["status"] = "fail"
                report["blocking_failures"].append("build")
                log("Build failed — aborting.")
                return report

        # Unit tests (no environment needed)
        if "unit-go" in active:
            log("=== UNIT TESTS: GO ===")
            result = run_unit_go()
            report["suites"]["unit-go"] = result
            if result["status"] == "fail":
                report["status"] = "fail"

        if "unit-elixir" in active:
            log("=== UNIT TESTS: ELIXIR ===")
            result = run_unit_elixir()
            report["suites"]["unit-elixir"] = result
            if result["status"] == "fail":
                report["status"] = "fail"

        # Environment-dependent suites
        if needs_env:
            log("=== ENVIRONMENT RESET ===")
            healthy, env_out = env_reset(args.no_reset, args.env_timeout)
            if not healthy:
                env_error = suite_result("fail", errors=["Environment startup failed or timed out"])
                if "e2e" in active:
                    report["suites"]["e2e"] = env_error
                if "integration" in active:
                    report["suites"]["integration"] = env_error
                report["status"] = "fail"
                report["blocking_failures"].append("env-startup")
                return report

            try:
                if "e2e" in active:
                    log("=== E2E TESTS ===")
                    result = run_e2e()
                    report["suites"]["e2e"] = result
                    if result["status"] == "fail":
                        report["status"] = "fail"

                if "integration" in active:
                    log("=== INTEGRATION TESTS ===")
                    result = run_integration()
                    report["suites"]["integration"] = result
                    if result["status"] == "fail":
                        report["status"] = "fail"
            finally:
                teardown()

    except subprocess.TimeoutExpired as e:
        report["status"] = "fail"
        report["blocking_failures"].append(f"timeout: {e}")
    except Exception as e:
        report["status"] = "fail"
        report["blocking_failures"].append(f"script-error: {e}")

    return report


if __name__ == "__main__":
    report = main()
    print(json.dumps(report, indent=2))
    sys.exit(0 if report["status"] == "pass" else 1)
