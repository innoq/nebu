#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# ///
"""
classify_story.py — Reads story file, outputs feature flags as JSON.

Usage:
  python3 classify_story.py --story PATH

Output (JSON to stdout):
  {"matrix": true|false, "ui": true|false, "security_review": "required|optional|not-needed"}

The script reads the story frontmatter for security_review. If missing, it
runs `git diff --staged --name-only` and checks for security-critical paths.
"""

import argparse
import json
import re
import subprocess
import sys

MATRIX_PATTERNS = re.compile(
    r"_matrix/|m\.room\.|m\.login|event_type|txnId|sync.*since|matrix.*spec",
    re.IGNORECASE,
)

SECURITY_PATHS = [
    "gateway/internal/auth/",
    "gateway/internal/middleware/",
    "gateway/internal/admin/",
    "gateway/internal/db/",
    "core/apps/signature/",
    "core/apps/permissions/",
]


def read_story(path):
    try:
        with open(path) as f:
            return f.read()
    except FileNotFoundError:
        print(f"Error: story file not found: {path}", file=sys.stderr)
        sys.exit(1)


def parse_frontmatter(content):
    match = re.match(r"^---\s*\n(.*?)\n---", content, re.DOTALL)
    if not match:
        return {}
    fields = {}
    for line in match.group(1).splitlines():
        m = re.match(r"^(\w+):\s*(.+)$", line)
        if m:
            fields[m.group(1).strip()] = m.group(2).strip()
    return fields


def get_staged_files():
    try:
        result = subprocess.run(
            ["git", "diff", "--staged", "--name-only"],
            capture_output=True, text=True, timeout=10,
        )
        return [l.strip() for l in result.stdout.splitlines() if l.strip()]
    except Exception:
        return []


def classify_security(frontmatter, staged_files):
    value = frontmatter.get("security_review", "").strip()
    if value in ("required", "optional", "not-needed"):
        return value

    # Auto-classify from staged files
    for f in staged_files:
        for path in SECURITY_PATHS:
            if f.startswith(path):
                return "required"
        if re.search(r"gateway/migrations/", f):
            return "required"
        if re.search(r"gateway/cmd/gateway/main\.go", f):
            return "required"

    return "not-needed"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--story", required=True, help="Path to story file")
    args = parser.parse_args()

    content = read_story(args.story)
    frontmatter = parse_frontmatter(content)
    staged = get_staged_files()

    result = {
        "matrix": bool(MATRIX_PATTERNS.search(content)),
        "ui": frontmatter.get("ui", "").lower() == "true",
        "security_review": classify_security(frontmatter, staged),
    }

    print(json.dumps(result))


if __name__ == "__main__":
    main()
