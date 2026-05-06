#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Unit tests for init_sanctum.py"""

import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))
from init_sanctum import parse_yaml_config, parse_frontmatter, substitute_vars, generate_capabilities_md


def test_parse_yaml_config():
    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
        f.write("user_name: Phil\n")
        f.flush()
        assert parse_yaml_config(Path(f.name))["user_name"] == "Phil"


def test_parse_yaml_config_missing():
    assert parse_yaml_config(Path("/nonexistent.yaml")) == {}


def test_parse_frontmatter():
    with tempfile.NamedTemporaryFile(mode="w", suffix=".md", delete=False) as f:
        f.write("---\nname: design-review\ncode: design-review\ndescription: Review UI\n---\n# Content")
        f.flush()
        result = parse_frontmatter(Path(f.name))
    assert result["code"] == "design-review"


def test_parse_frontmatter_no_frontmatter():
    with tempfile.NamedTemporaryFile(mode="w", suffix=".md", delete=False) as f:
        f.write("# No frontmatter")
        f.flush()
        assert parse_frontmatter(Path(f.name)) == {}


def test_substitute_vars():
    result = substitute_vars("Hello {user_name}", {"user_name": "Phil"})
    assert result == "Hello Phil"


def test_generate_capabilities_evolvable():
    caps = [{"code": "design-review", "name": "Design Review", "description": "Review UI", "source": "./references/design-review.md"}]
    result = generate_capabilities_md(caps, evolvable=True)
    assert "design-review" in result
    assert "Learned" in result
    assert "Playwright" in result


def test_generate_capabilities_not_evolvable():
    caps = [{"code": "design-review", "name": "Design Review", "description": "Review UI", "source": "./references/design-review.md"}]
    result = generate_capabilities_md(caps, evolvable=False)
    assert "Learned" not in result


if __name__ == "__main__":
    tests = [
        test_parse_yaml_config, test_parse_yaml_config_missing,
        test_parse_frontmatter, test_parse_frontmatter_no_frontmatter,
        test_substitute_vars,
        test_generate_capabilities_evolvable, test_generate_capabilities_not_evolvable,
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
