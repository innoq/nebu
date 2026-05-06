#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Unit tests for init-sanctum.py"""

import sys
import tempfile
import json
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))
from init_sanctum import (
    parse_yaml_config,
    parse_frontmatter,
    substitute_vars,
    generate_capabilities_md,
)


def test_parse_yaml_config():
    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
        f.write("user_name: Phil\ncommunication_language: German\n")
        f.flush()
        result = parse_yaml_config(Path(f.name))
    assert result["user_name"] == "Phil"
    assert result["communication_language"] == "German"


def test_parse_yaml_config_missing_file():
    result = parse_yaml_config(Path("/nonexistent/path.yaml"))
    assert result == {}


def test_parse_frontmatter():
    with tempfile.NamedTemporaryFile(mode="w", suffix=".md", delete=False) as f:
        f.write("---\nname: spec-lookup\ncode: spec-lookup\ndescription: Test cap\n---\n\n# Content")
        f.flush()
        result = parse_frontmatter(Path(f.name))
    assert result["name"] == "spec-lookup"
    assert result["code"] == "spec-lookup"
    assert result["description"] == "Test cap"


def test_parse_frontmatter_no_frontmatter():
    with tempfile.NamedTemporaryFile(mode="w", suffix=".md", delete=False) as f:
        f.write("# Just content, no frontmatter")
        f.flush()
        result = parse_frontmatter(Path(f.name))
    assert result == {}


def test_substitute_vars():
    content = "Hello {user_name}, born on {birth_date}."
    result = substitute_vars(content, {"user_name": "Phil", "birth_date": "2026-05-06"})
    assert result == "Hello Phil, born on 2026-05-06."


def test_generate_capabilities_md_with_evolvable():
    caps = [{"code": "spec-lookup", "name": "Spec Lookup", "description": "Look up specs", "source": "./references/spec-lookup.md"}]
    result = generate_capabilities_md(caps, evolvable=True)
    assert "spec-lookup" in result
    assert "Learned" in result
    assert "How to Add a Capability" in result


def test_generate_capabilities_md_without_evolvable():
    caps = [{"code": "spec-lookup", "name": "Spec Lookup", "description": "Look up specs", "source": "./references/spec-lookup.md"}]
    result = generate_capabilities_md(caps, evolvable=False)
    assert "spec-lookup" in result
    assert "Learned" not in result


if __name__ == "__main__":
    tests = [
        test_parse_yaml_config,
        test_parse_yaml_config_missing_file,
        test_parse_frontmatter,
        test_parse_frontmatter_no_frontmatter,
        test_substitute_vars,
        test_generate_capabilities_md_with_evolvable,
        test_generate_capabilities_md_without_evolvable,
    ]
    passed = 0
    failed = 0
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
