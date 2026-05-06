#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# ///
"""
update_sprint_status.py — Mark a story done in sprint-status.yaml.

Prepends a done-comment at the top of the file and updates the story's
status entry in the YAML block. Never removes existing lines.

Usage:
  python3 update_sprint_status.py --file PATH --story ID --summary TEXT [--status STATUS]

Arguments:
  --file PATH       Path to sprint-status.yaml
  --story ID        Story ID, e.g. "9-22"
  --summary TEXT    Pipeline summary for the comment, e.g. "ATDD+Code CLEAN"
  --status STATUS   New status value (default: done)
  --date DATE       Date string (default: today, YYYY-MM-DD)
"""

import argparse
import re
import sys
from datetime import date


def read_file(path):
    try:
        with open(path) as f:
            return f.readlines()
    except FileNotFoundError:
        print(f"Error: {path} not found", file=sys.stderr)
        sys.exit(1)


def write_file(path, lines):
    with open(path, "w") as f:
        f.writelines(lines)


def split_blocks(lines):
    """Split into leading comment block and the rest."""
    i = 0
    while i < len(lines) and (lines[i].startswith("#") or lines[i].strip() == ""):
        i += 1
    return lines[:i], lines[i:]


def update_last_updated_comment(comment_lines, date_str):
    for i, line in enumerate(comment_lines):
        if line.startswith("# last_updated:"):
            comment_lines[i] = f"# last_updated: {date_str}\n"
            return


def prepend_done_comment(comment_lines, story_id, summary, date_str):
    """Insert done-comment directly after # last_updated: line."""
    entry = f"# story {story_id} done (pipeline: {summary}): {date_str}\n"
    for i, line in enumerate(comment_lines):
        if line.startswith("# last_updated:"):
            comment_lines.insert(i + 1, entry)
            return
    # Fallback: insert after header
    comment_lines.insert(min(2, len(comment_lines)), entry)


def update_story_status(yaml_lines, story_id, new_status):
    """Find the story entry by ID prefix and update its status."""
    # Story lines look like: "  9-22-some-slug: backlog"
    pattern = re.compile(rf"^(\s+{re.escape(story_id)}-[a-zA-Z0-9_-]+:\s*)(\S+)(\s*)$")
    for i, line in enumerate(yaml_lines):
        m = pattern.match(line)
        if m:
            yaml_lines[i] = f"{m.group(1)}{new_status}{m.group(3) or chr(10)}"
            return True
    return False


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--file", required=True)
    parser.add_argument("--story", required=True, help="Story ID, e.g. 9-22")
    parser.add_argument("--summary", required=True, help="Pipeline summary for log comment")
    parser.add_argument("--status", default="done", help="New status value (default: done)")
    parser.add_argument("--date", default=None, help="Date string (default: today)")
    args = parser.parse_args()

    date_str = args.date or date.today().isoformat()

    lines = read_file(args.file)
    comment_lines, yaml_lines = split_blocks(lines)

    update_last_updated_comment(comment_lines, date_str)
    prepend_done_comment(comment_lines, args.story, args.summary, date_str)

    found = update_story_status(yaml_lines, args.story, args.status)
    if not found:
        print(f"Warning: story entry for '{args.story}' not found in YAML block", file=sys.stderr)

    write_file(args.file, comment_lines + yaml_lines)
    print(f"✓ {args.file}: story {args.story} → {args.status}")


if __name__ == "__main__":
    main()
