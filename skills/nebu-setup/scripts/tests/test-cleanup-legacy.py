#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# dependencies = []
# ///
"""Unit tests for cleanup-legacy.py"""

import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))
from cleanup_legacy import find_skill_dirs


def test_find_skill_dirs_empty_dir():
    with tempfile.TemporaryDirectory() as tmpdir:
        result = find_skill_dirs(tmpdir)
    assert result == []


def test_find_skill_dirs_nonexistent():
    result = find_skill_dirs("/nonexistent/path")
    assert result == []


def test_find_skill_dirs_finds_skill():
    with tempfile.TemporaryDirectory() as tmpdir:
        skill_dir = Path(tmpdir) / "some-skill"
        skill_dir.mkdir()
        (skill_dir / "SKILL.md").write_text("---\nname: test\n---\n")
        result = find_skill_dirs(tmpdir)
    assert "some-skill" in result


def test_find_skill_dirs_finds_multiple_skills():
    with tempfile.TemporaryDirectory() as tmpdir:
        for name in ["skill-a", "skill-b", "skill-c"]:
            d = Path(tmpdir) / name
            d.mkdir()
            (d / "SKILL.md").write_text("---\nname: test\n---\n")
        result = find_skill_dirs(tmpdir)
    assert len(result) == 3
    assert "skill-a" in result and "skill-b" in result and "skill-c" in result


def test_find_skill_dirs_ignores_non_skill_dirs():
    with tempfile.TemporaryDirectory() as tmpdir:
        skill_dir = Path(tmpdir) / "my-skill"
        skill_dir.mkdir()
        (skill_dir / "SKILL.md").write_text("---\nname: test\n---\n")
        # Non-skill dir — no SKILL.md
        other_dir = Path(tmpdir) / "not-a-skill"
        other_dir.mkdir()
        (other_dir / "config.yaml").write_text("key: value\n")
        result = find_skill_dirs(tmpdir)
    assert "my-skill" in result
    assert "not-a-skill" not in result


def test_find_skill_dirs_deduplicates():
    with tempfile.TemporaryDirectory() as tmpdir:
        nested = Path(tmpdir) / "outer" / "same-skill"
        nested.mkdir(parents=True)
        (nested / "SKILL.md").write_text("---\nname: test\n---\n")
        result = find_skill_dirs(tmpdir)
    # Returns unique names
    assert result.count("same-skill") == 1


if __name__ == "__main__":
    tests = [
        test_find_skill_dirs_empty_dir, test_find_skill_dirs_nonexistent,
        test_find_skill_dirs_finds_skill, test_find_skill_dirs_finds_multiple_skills,
        test_find_skill_dirs_ignores_non_skill_dirs, test_find_skill_dirs_deduplicates,
    ]
    passed = failed = 0
    for test in tests:
        try:
            test()
            print(f"  ✓ {test.__name__}")
            passed += 1
        except Exception as e:
            print(f"  ✗ {test.__name__}: {e}")
            failed += 1
    print(f"\n{passed} passed, {failed} failed")
    sys.exit(0 if failed == 0 else 1)
