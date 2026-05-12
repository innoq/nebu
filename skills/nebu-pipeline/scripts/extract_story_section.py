#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# ///
"""
extract_story_section.py — Extract specific sections from a Markdown story file.

Usage:
  python3 extract_story_section.py --story PATH --sections "Acceptance Criteria" "Dev Notes"
  python3 extract_story_section.py --story PATH --all

--sections: extract named sections in the given order (case-insensitive heading match).
--all:      extract ALL level-2 (##) sections in document order. Use as context fallback
            when a step signals missing story knowledge. Never extract less than the
            previous call — always pass at minimum the previously used sections.

Output: the requested sections (including their headings) to stdout, separated by a blank line.
Missing sections produce a warning on stderr but do not abort (exit 0).
"""

import argparse
import re
import sys
from pathlib import Path


def build_toc(lines: list) -> list:
    """Return list of (line_idx, level, name) for all headings."""
    heading_re = re.compile(r"^(#{1,6})\s+(.+)$")
    toc = []
    for i, line in enumerate(lines):
        m = heading_re.match(line)
        if m:
            toc.append((i, len(m.group(1)), m.group(2).strip()))
    return toc


def section_content(lines: list, toc: list, j: int) -> str:
    """Return the full content of toc entry j, up to the next same-or-higher heading."""
    start_idx, level, _ = toc[j]
    end_idx = len(lines)
    for k in range(j + 1, len(toc)):
        next_idx, next_level, _ = toc[k]
        if next_level <= level:
            end_idx = next_idx
            break
    return "\n".join(lines[start_idx:end_idx]).rstrip()


def extract_named(content: str, targets: list) -> str:
    lines = content.splitlines()
    toc = build_toc(lines)
    parts = []
    for target in targets:
        found = False
        for j, (_, _, name) in enumerate(toc):
            if name.lower() == target.lower():
                found = True
                parts.append(section_content(lines, toc, j))
                break
        if not found:
            print(f"Warning: section '{target}' not found in story file", file=sys.stderr)
    return "\n\n".join(parts)


def extract_all(content: str) -> str:
    """Extract all level-2 (##) sections in document order."""
    lines = content.splitlines()
    toc = build_toc(lines)
    parts = []
    for j, (_, level, _) in enumerate(toc):
        if level == 2:
            parts.append(section_content(lines, toc, j))
    return "\n\n".join(parts)


def main():
    parser = argparse.ArgumentParser(
        description="Extract named sections from a Markdown story file"
    )
    parser.add_argument("--story", required=True, help="Path to story file")
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--sections", nargs="+", help="Section heading names to extract")
    group.add_argument("--all", action="store_true", help="Extract all level-2 sections (context fallback)")
    args = parser.parse_args()

    story_path = Path(args.story)
    if not story_path.exists():
        print(f"Error: story file not found: {args.story}", file=sys.stderr)
        sys.exit(1)

    content = story_path.read_text(encoding="utf-8")
    result = extract_all(content) if args.all else extract_named(content, args.sections)

    if result:
        print(result)
    else:
        print("Warning: no sections extracted — check section names match story headings", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
