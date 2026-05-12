#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# ///
"""
select_pipeline_step.py — Extract a step prompt from pipeline-steps-spec.md.

Usage:
  python3 select_pipeline_step.py --step atdd
  python3 select_pipeline_step.py --step code-review --spec path/to/pipeline-steps-spec.md

Reads pipeline-steps-spec.md (default: sibling of this script's parent directory)
and extracts the ## <step> section. Output goes to stdout for inline use in subagent prompts.

The coordinator (SKILL.md) injects runtime values for [BRACKETED] placeholders.
"""

import argparse
import re
import sys
from pathlib import Path

DEFAULT_SPEC = Path(__file__).parent.parent / "pipeline-steps-spec.md"


def build_toc(lines: list) -> list:
    heading_re = re.compile(r"^(#{1,6})\s+(.+)$")
    toc = []
    for i, line in enumerate(lines):
        m = heading_re.match(line)
        if m:
            toc.append((i, len(m.group(1)), m.group(2).strip()))
    return toc


def extract_step(content: str, step: str) -> str:
    lines = content.splitlines()
    toc = build_toc(lines)

    for j, (start_idx, level, name) in enumerate(toc):
        if name.lower() == step.lower():
            end_idx = len(lines)
            for k in range(j + 1, len(toc)):
                next_idx, next_level, _ = toc[k]
                if next_level <= level:
                    end_idx = next_idx
                    break
            # Strip the heading line itself — return only the prompt body
            body_lines = lines[start_idx + 1:end_idx]
            # Trim leading/trailing blank lines and section separators
            while body_lines and not body_lines[0].strip():
                body_lines.pop(0)
            while body_lines and (not body_lines[-1].strip() or body_lines[-1].strip() == "---"):
                body_lines.pop()
            return "\n".join(body_lines)

    return ""


def main():
    parser = argparse.ArgumentParser(
        description="Extract a step prompt from pipeline-steps-spec.md"
    )
    parser.add_argument("--step", required=True, help="Step name (e.g. atdd, code-review)")
    parser.add_argument(
        "--spec",
        default=str(DEFAULT_SPEC),
        help=f"Path to pipeline-steps-spec.md (default: {DEFAULT_SPEC})",
    )
    args = parser.parse_args()

    spec_path = Path(args.spec)
    if not spec_path.exists():
        print(f"Error: spec file not found: {args.spec}", file=sys.stderr)
        sys.exit(1)

    content = spec_path.read_text(encoding="utf-8")
    result = extract_step(content, args.step)

    if result:
        print(result)
    else:
        print(f"Error: step '{args.step}' not found in {args.spec}", file=sys.stderr)
        available = [name for _, level, name in build_toc(content.splitlines()) if level == 2]
        if available:
            print(f"Available steps: {', '.join(available)}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
