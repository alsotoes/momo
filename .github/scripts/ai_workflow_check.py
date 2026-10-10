#!/usr/bin/env python3
"""
ai_workflow_check.py — Validates the harness manifest (openspec/ai-workflow.yaml)
against the live opencode.json and global config, ensuring:
1. All plugins in opencode.json (project + global) are declared in the manifest
2. Declared pinned versions are not floating (@latest)
3. Ponytail policy: default_mode = lite; ultra not in allowed
4. No secrets in opencode.json or manifest
5. Referenced config files exist
6. Referenced steering rule IDs exist in openspec/config.yaml
7. Global MCP set == declared global set (mempalace only)
"""

import json
import os
import re
import sys
import yaml
from pathlib import Path
from typing import Dict, List, Set, Tuple, Any, Optional

ROOT = Path(__file__).resolve().parents[2]  # repo root
MANIFEST_PATH = ROOT / "openspec" / "ai-workflow.yaml"
OPENCODE_PROJECT = ROOT / "opencode.json"
OPENCODE_GLOBAL = Path.home() / ".config" / "opencode" / "opencode.json"
CONFIG_YAML = ROOT / "openspec" / "config.yaml"
ECC_TOOLS_YML = ROOT / ".github" / "ecc-tools.yml"
MEMPALACE_YAML = ROOT / "mempalace.yaml"


def load_yaml(path: Path) -> dict:
    if not path.exists():
        return {}
    return yaml.safe_load(path.read_text()) or {}


def load_json(path: Path) -> dict:
    if not path.exists():
        return {}
    return json.loads(path.read_text()) or {}


def get_steering_rules() -> Set[int]:
    """Extract rule numbers from openspec/config.yaml.

    The config stores rules as a markdown-formatted string under the `context`
    key, with numbered bold headings like `  73. **Spec-First ...**` — so the
    reliable parse is a regex over the raw text, not a dict walk.
    """
    try:
        raw = CONFIG_YAML.read_text()
    except OSError:
        return set()
    return {int(m.group(1)) for m in re.finditer(r'^\s*(\d+)\.\s+\*\*', raw, re.MULTILINE)}


def check_plugins() -> List[str]:
    """Validate plugin declarations against manifest"""
    errors = []
    warnings = []

    manifest = load_yaml(Path(MANIFEST_PATH))
    project_cfg = load_json(OPENCODE_PROJECT)
    global_cfg = load_json(OPENCODE_GLOBAL)

    # Collect all plugins from manifest
    declared_plugins = set()
    plugin_info = {}  # name -> info dict
    for plugin in manifest.get("plugins", []):
        name = plugin.get("package")
        if name:
            base = name.rsplit("@", 1)[0] if not name.startswith("@") or name.count("@") > 1 else name
            declared_plugins.add(base)
            plugin_info[base] = plugin

    # Check project opencode.json
    project_plugins = project_cfg.get("plugins", [])
    project_plugin_names = [p if isinstance(p, str) else p.get("package") for p in project_plugins if p]

    # Check global opencode.json
    global_plugins = global_cfg.get("plugin", [])  # V1 key
    global_plugins = global_cfg.get("plugins", []) if not global_plugins else global_plugins
    global_plugin_names = [p if isinstance(p, str) else p.get("package") for p in global_plugins if p]

    # opencode.json entries carry a version suffix ("pkg@1.2.3") while the
    # manifest declares the bare package name ("pkg"), so compare on the name.
    def plugin_name(spec: str) -> str:
        if not isinstance(spec, str):
            return ""
        return spec.split("@")[0] if spec.startswith("@") and spec.count("@") == 1 else spec.rsplit("@", 1)[0]

    # 1. All plugins in project config must be declared in manifest
    for p in project_plugin_names:
        if plugin_name(p) not in declared_plugins:
            errors.append(f"Plugin '{p}' in opencode.json is not declared in manifest")

    # 2. All plugins in global config must be declared in manifest
    for p in global_plugin_names:
        if plugin_name(p) not in declared_plugins:
            warnings.append(f"Global plugin '{p}' not declared in manifest (local-only check skipped in CI)")

    # 3. Check pinned versions (no @latest unless floating: true)
    for name, info in plugin_info.items():
        pinned = info.get("pinned")
        if pinned and "@latest" in str(pinned):
            errors.append(f"Plugin '{name}' uses @latest but must be pinned (no floating unless floating: true)")

    # 4. Ponytail policy checks
    ponytail = plugin_info.get("ponytail") or plugin_info.get("@dietrichgebert/ponytail")
    if ponytail:
        # default_mode must be lite
        if ponytail.get("default_mode") != "lite":
            errors.append("Ponytail default_mode must be 'lite'")
        # ultra must be forbidden
        forbidden = ponytail.get("forbidden", [])
        if "ultra-mode" not in forbidden and "ultra" not in forbidden:
            errors.append("Ponytail 'ultra-mode' must be in forbidden list")
        # ultra must not be in allowed
        allowed = ponytail.get("allowed", [])
        if "ultra-mode" in allowed or "ultra" in allowed:
            errors.append("Ponytail 'ultra-mode' must not be in allowed list")

    # 5. Check for secrets in manifest and opencode.json
    manifest_str = yaml.dump(manifest)
    project_str = json.dumps(project_cfg)
    for secret_pattern in [r"sk-[a-zA-Z0-9]{32,}", r"ghp_[a-zA-Z0-9]{36,}", r"[a-zA-Z0-9]{24}\.[a-zA-Z0-9]{6}\.[a-zA-Z0-9_=-]{27}"]:
        if re.search(secret_pattern, manifest_str):
            errors.append(f"Possible secret found in manifest")
        if re.search(secret_pattern, project_str):
            errors.append(f"Possible secret found in opencode.json")

    # 6. Referenced config files exist
    for path_key, path_obj in [("ecc-tools.yml", ECC_TOOLS_YML), ("mempalace.yaml", MEMPALACE_YAML)]:
        if not path_obj.exists():
            errors.append(f"Referenced config file {path_key} does not exist at {path_obj}")

    # 6b. Referenced steering rule IDs exist
    rules = get_steering_rules()
    # Extract rule refs from manifest (rules arrays)
    rule_refs = set()
    for plugin in manifest.get("plugins", []):
        for rule_ref in plugin.get("rules", []):
            if isinstance(rule_ref, int):
                rule_refs.add(str(rule_ref))
            else:
                rule_refs.add(str(rule_ref).replace("Rule ", "").replace("rule-", ""))
    for harness in manifest.get("harness", {}).values():
        for rule_ref in harness.get("rules", []):
            if isinstance(rule_ref, int):
                rule_refs.add(str(rule_ref))
            else:
                rule_refs.add(str(rule_ref).replace("Rule ", "").replace("rule-", ""))
    for inv in manifest.get("invariants", []):
        # invariants may mention rule IDs in text
        for word in str(inv).split():
            if word.isdigit() or word.startswith("Rule "):
                rule_refs.add(word.replace("Rule ", ""))
    for ref in rule_refs:
        if ref.isdigit() and int(ref) not in rules:
            warnings.append(f"Manifest references rule #{ref} which may not exist in config.yaml")

    # 7. Global MCP set consistency (only in local run where global config exists)
    if Path(OPENCODE_GLOBAL).exists():
        global_cfg = load_json(OPENCODE_GLOBAL)
        global_mcp = global_cfg.get("mcp", {})
        declared_global = {p for p, info in plugin_info.items() if info.get("scope") == "global"}
        for name in global_mcp:
            if name not in declared_global:
                warnings.append(f"Global MCP '{name}' not declared in manifest")

    return errors, warnings


def main():
    print(f"Checking {MANIFEST_PATH}...")
    errors, warnings = check_plugins()

    if warnings:
        print("WARNINGS:")
        for w in warnings:
            print(f"  - {w}")

    if errors:
        print("ERRORS:")
        for e in errors:
            print(f"  - {e}")
        sys.exit(1)
    else:
        print("✅ ai-workflow check passed")
        sys.exit(0)


if __name__ == "__main__":
    main()
