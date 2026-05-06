#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# ///
"""
pipeline_state.py — Atomic read/modify/write for pipeline-state.yaml.

Prepends log lines, updates YAML fields, zeroes fields on commit.
Never removes existing comment/log lines — they are the permanent journal.

Usage:
  python3 pipeline_state.py --file PATH [options]

Options:
  --log TEXT        Prepend log line (# prefix added automatically)
  --story ID        Set story field
  --step STEP       Set current_step field
  --done STEP       Append step to completed list (repeatable)
  --cycles N        Set cycle_count field
  --blocked REASON  Set blocked_reason field (use "null" to clear)
  --timestamp ISO   Set last_updated (default: current UTC)
  --commit          Zero all YAML fields, keep all comment/log lines

Multiple flags combine in one call:
  pipeline_state.py --file path --step atdd --done create-story --log "[9-1] ... create-story → story.md"
"""

import argparse
import re
import sys
from datetime import datetime, timezone


def now_iso():
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%MZ")


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
    """Split into comment block (leading # lines) and yaml block (the rest)."""
    i = 0
    while i < len(lines) and (lines[i].startswith("#") or lines[i].strip() == ""):
        i += 1
    return lines[:i], lines[i:]


def update_last_updated_comment(comment_lines, date_str):
    for i, line in enumerate(comment_lines):
        if line.startswith("# last_updated:"):
            comment_lines[i] = f"# last_updated: {date_str}\n"
            return
    # Insert after header lines (first two)
    insert_at = min(2, len(comment_lines))
    comment_lines.insert(insert_at, f"# last_updated: {date_str}\n")


def prepend_log_line(comment_lines, log_text):
    """Insert log line directly after # last_updated: line."""
    for i, line in enumerate(comment_lines):
        if line.startswith("# last_updated:"):
            comment_lines.insert(i + 1, f"# {log_text}\n")
            return
    comment_lines.append(f"# {log_text}\n")


def set_scalar(yaml_lines, field, value):
    """Replace or append a scalar YAML field."""
    pattern = re.compile(rf"^{re.escape(field)}:")
    if value is None:
        new_line = f"{field}: null\n"
    elif isinstance(value, int):
        new_line = f"{field}: {value}\n"
    else:
        new_line = f'{field}: "{value}"\n'
    for i, line in enumerate(yaml_lines):
        if pattern.match(line):
            yaml_lines[i] = new_line
            return
    yaml_lines.append(new_line)


def append_completed(yaml_lines, step):
    """Append a step to the completed list (idempotent). Handles both block and inline [] forms."""
    completed_idx = next((i for i, l in enumerate(yaml_lines) if l.startswith("completed:")), None)
    if completed_idx is None:
        yaml_lines.append(f"completed:\n  - {step}\n")
        return

    line = yaml_lines[completed_idx]

    # Inline form: "completed: []" — convert to block form first
    if re.match(r"completed:\s*\[\s*\]", line):
        yaml_lines[completed_idx] = f"completed:\n  - {step}\n"
        return

    # Inline form with items: "completed: [a, b]" — parse and convert
    inline_match = re.match(r"completed:\s*\[(.+)\]", line)
    if inline_match:
        items = [i.strip().strip('"\'') for i in inline_match.group(1).split(",") if i.strip()]
        if step not in items:
            items.append(step)
        yaml_lines[completed_idx] = "completed:\n" + "".join(f"  - {i}\n" for i in items)
        return

    # Block form: find end and insert
    insert_idx = completed_idx + 1
    while insert_idx < len(yaml_lines) and yaml_lines[insert_idx].startswith("  - "):
        if yaml_lines[insert_idx].strip().lstrip("- ") == step:
            return  # already present
        insert_idx += 1

    yaml_lines.insert(insert_idx, f"  - {step}\n")


def clear_completed(yaml_lines):
    """Replace completed list with empty form."""
    completed_idx = next((i for i, l in enumerate(yaml_lines) if l.startswith("completed:")), None)
    if completed_idx is None:
        return
    end = completed_idx + 1
    while end < len(yaml_lines) and yaml_lines[end].startswith("  - "):
        end += 1
    del yaml_lines[completed_idx + 1:end]
    yaml_lines[completed_idx] = "completed: []\n"


def main():
    parser = argparse.ArgumentParser(description="Update pipeline-state.yaml atomically.")
    parser.add_argument("--file", required=True, help="Path to pipeline-state.yaml")
    parser.add_argument("--log", default=None, help="Log text to prepend (without # prefix)")
    parser.add_argument("--story", default=None, help="Set story field")
    parser.add_argument("--step", default=None, help="Set current_step field")
    parser.add_argument("--done", action="append", default=[], metavar="STEP",
                        help="Append to completed list (repeatable)")
    parser.add_argument("--cycles", type=int, default=None, help="Set cycle_count")
    parser.add_argument("--blocked", default=None, help="Set blocked_reason ('null' to clear)")
    parser.add_argument("--timestamp", default=None, help="ISO timestamp (default: current UTC)")
    parser.add_argument("--commit", action="store_true", help="Zero all YAML fields on story completion")
    args = parser.parse_args()

    ts = args.timestamp or now_iso()
    date_only = ts[:10]

    lines = read_file(args.file)
    comment_lines, yaml_lines = split_blocks(lines)

    # Comment block updates
    update_last_updated_comment(comment_lines, date_only)
    if args.log:
        prepend_log_line(comment_lines, args.log)

    # YAML field updates
    if args.commit:
        set_scalar(yaml_lines, "story", None)
        set_scalar(yaml_lines, "current_step", None)
        clear_completed(yaml_lines)
        set_scalar(yaml_lines, "cycle_count", 0)
        set_scalar(yaml_lines, "blocked_reason", None)
        set_scalar(yaml_lines, "last_updated", ts)
    else:
        if args.story:
            set_scalar(yaml_lines, "story", args.story)
        if args.step:
            set_scalar(yaml_lines, "current_step", args.step)
        for step in args.done:
            append_completed(yaml_lines, step)
        if args.cycles is not None:
            set_scalar(yaml_lines, "cycle_count", args.cycles)
        if args.blocked is not None:
            set_scalar(yaml_lines, "blocked_reason",
                       None if args.blocked == "null" else args.blocked)
        set_scalar(yaml_lines, "last_updated", ts)

    write_file(args.file, comment_lines + yaml_lines)
    print(f"✓ {args.file} updated")


if __name__ == "__main__":
    main()
