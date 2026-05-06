#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# dependencies = []
# ///
"""Unit tests for merge-help-csv.py"""

import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))
from merge_help_csv import read_csv_rows, filter_rows, extract_module_codes, write_csv, HEADER


def test_read_csv_rows_missing_file():
    header, rows = read_csv_rows("/nonexistent/file.csv")
    assert header == []
    assert rows == []


def test_read_csv_rows_with_data():
    with tempfile.NamedTemporaryFile(mode="w", suffix=".csv", delete=False) as f:
        f.write("module,skill,display-name\nnebu,nebu-pipeline,Pipeline\n")
        f.flush()
        header, rows = read_csv_rows(f.name)
    assert header == ["module", "skill", "display-name"]
    assert len(rows) == 1
    assert rows[0][0] == "nebu"


def test_extract_module_codes():
    rows = [["nebu", "nebu-pipeline", "Pipeline"], ["nebu", "nebu-agent-oracle", "Oracle"]]
    codes = extract_module_codes(rows)
    assert codes == {"nebu"}


def test_extract_module_codes_empty():
    assert extract_module_codes([]) == set()


def test_filter_rows_removes_matching():
    rows = [["nebu", "nebu-pipeline"], ["bmb", "bmad-agent-builder"], ["nebu", "nebu-agent-oracle"]]
    filtered = filter_rows(rows, "nebu")
    assert len(filtered) == 1
    assert filtered[0][0] == "bmb"


def test_filter_rows_keeps_all_when_no_match():
    rows = [["nebu", "nebu-pipeline"], ["nebu", "nebu-agent-oracle"]]
    filtered = filter_rows(rows, "other")
    assert len(filtered) == 2


def test_write_csv_creates_file():
    with tempfile.TemporaryDirectory() as tmpdir:
        target = Path(tmpdir) / "out.csv"
        rows = [["nebu", "nebu-pipeline", "Pipeline", "pipeline", "desc", "", "", "", "", "", "", "", ""]]
        write_csv(str(target), HEADER, rows)
        assert target.exists()
        content = target.read_text()
        assert "nebu" in content
        assert "nebu-pipeline" in content


def test_write_csv_anti_zombie():
    with tempfile.TemporaryDirectory() as tmpdir:
        target = Path(tmpdir) / "help.csv"
        # Write initial
        rows1 = [["nebu", "nebu-pipeline", "Old Pipeline", "pipeline", "", "", "", "", "", "", "", "", ""]]
        write_csv(str(target), HEADER, rows1)
        # Overwrite with new content (anti-zombie applied by caller — write_csv just writes)
        rows2 = [["nebu", "nebu-pipeline", "New Pipeline", "pipeline", "", "", "", "", "", "", "", "", ""]]
        write_csv(str(target), HEADER, rows2)
        content = target.read_text()
        assert "New Pipeline" in content
        assert content.count("nebu-pipeline") == 1


if __name__ == "__main__":
    tests = [
        test_read_csv_rows_missing_file, test_read_csv_rows_with_data,
        test_extract_module_codes, test_extract_module_codes_empty,
        test_filter_rows_removes_matching, test_filter_rows_keeps_all_when_no_match,
        test_write_csv_creates_file, test_write_csv_anti_zombie,
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
