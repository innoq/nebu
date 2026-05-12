#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# dependencies = ["pyyaml"]
# ///
"""Unit tests for merge-config.py"""

import sys
import tempfile
from pathlib import Path

try:
    import yaml
except ImportError:
    print("pyyaml required — run: pip install pyyaml")
    sys.exit(2)

sys.path.insert(0, str(Path(__file__).parent.parent))
from merge_config import load_yaml_file


def test_load_yaml_file_missing():
    result = load_yaml_file("/nonexistent/config.yaml")
    assert result == {}


def test_load_yaml_file_empty():
    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
        f.write("")
        f.flush()
        result = load_yaml_file(f.name)
    assert result == {}


def test_load_yaml_file_simple_keys():
    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
        f.write("user_name: Phil\ncommunication_language: English\n")
        f.flush()
        result = load_yaml_file(f.name)
    assert result["user_name"] == "Phil"
    assert result["communication_language"] == "English"


def test_load_yaml_file_nested():
    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
        f.write("nebu:\n  module_version: 1.0.0\n")
        f.flush()
        result = load_yaml_file(f.name)
    assert result["nebu"]["module_version"] == "1.0.0"


def test_module_yaml_is_valid():
    module_yaml_path = Path(__file__).parent.parent.parent / "assets" / "module.yaml"
    assert module_yaml_path.exists(), f"module.yaml not found at {module_yaml_path}"
    result = load_yaml_file(str(module_yaml_path))
    assert result.get("code") == "nebu"
    assert result.get("name") == "Nebu Dev"
    assert "module_version" in result


def test_module_yaml_has_greeting():
    module_yaml_path = Path(__file__).parent.parent.parent / "assets" / "module.yaml"
    result = load_yaml_file(str(module_yaml_path))
    assert "module_greeting" in result
    assert len(result["module_greeting"]) > 0


if __name__ == "__main__":
    tests = [
        test_load_yaml_file_missing, test_load_yaml_file_empty,
        test_load_yaml_file_simple_keys, test_load_yaml_file_nested,
        test_module_yaml_is_valid, test_module_yaml_has_greeting,
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
