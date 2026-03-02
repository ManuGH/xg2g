#!/usr/bin/env python3

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
REGISTRY_PATH = ROOT / "docs/deprecations.json"
POLICY_PATH = ROOT / "docs/DEPRECATION_POLICY.md"
CONFIG_PATH = ROOT / "internal/config/deprecation.go"
AUTH_TOKEN_PATH = ROOT / "internal/auth/token.go"
# Deprecated configuration usage should be annotated with a deprecation_id.
# The registry is the single source of truth for operator-facing settings.
CONFIG_DEPRECATED_SETTINGS_PATH = ROOT / "internal/config/registry.go"

SEMVER_RE = re.compile(r"^v\d+\.\d+\.\d+$")
HEADER_RE = re.compile(r"^##\s+")
DEPRECATED_ID_RE = re.compile(r"deprecation_id=([a-z0-9_-]+)")


def add_error(errors, message):
    errors.append(message)


def load_registry(errors):
    if not REGISTRY_PATH.exists():
        add_error(errors, f"Missing registry: {REGISTRY_PATH}")
        return []

    try:
        data = json.loads(REGISTRY_PATH.read_text())
    except Exception as exc:
        add_error(errors, f"Invalid JSON in {REGISTRY_PATH}: {exc}")
        return []

    if data.get("version") != 1:
        add_error(errors, f"Unexpected registry version in {REGISTRY_PATH}: {data.get('version')}")

    deprecations = data.get("deprecations")
    if not isinstance(deprecations, list):
        add_error(errors, f"Registry has invalid deprecations list: {REGISTRY_PATH}")
        return []

    ids = set()
    for dep in deprecations:
        for key in ("id", "item", "replacement", "remove_in"):
            if key not in dep:
                add_error(errors, f"Registry entry missing '{key}': {dep}")
        dep_id = dep.get("id")
        if not isinstance(dep_id, str) or not dep_id:
            add_error(errors, f"Registry entry has invalid id: {dep}")
        else:
            if dep_id in ids:
                add_error(errors, f"Duplicate registry id: {dep_id}")
            ids.add(dep_id)

        remove_in = dep.get("remove_in")
        if not isinstance(remove_in, str) or not SEMVER_RE.match(remove_in):
            add_error(errors, f"Invalid remove_in for {dep_id}: {remove_in}")

    return deprecations


def parse_policy_table(errors):
    if not POLICY_PATH.exists():
        add_error(errors, f"Missing policy doc: {POLICY_PATH}")
        return []

    lines = POLICY_PATH.read_text().splitlines()
    start_idx = None
    for idx, line in enumerate(lines):
        if line.strip() == "## Active Deprecations":
            start_idx = idx + 1
            break

    if start_idx is None:
        add_error(errors, f"Missing '## Active Deprecations' section in {POLICY_PATH}")
        return []

    rows = []
    saw_none = False
    for line in lines[start_idx:]:
        if HEADER_RE.match(line.strip()):
            break
        if not line.strip():
            continue
        if line.strip().lower() in {"none", "none."}:
            saw_none = True
            break
        if not line.lstrip().startswith("|"):
            continue

        cells = [cell.strip() for cell in line.strip().strip("|").split("|")]
        if len(cells) != 3:
            add_error(errors, f"Malformed table row in {POLICY_PATH}: {line}")
            continue

        if cells[0].lower() == "item" and cells[1].lower() == "replacement":
            continue

        if all(cell.replace(":", "").replace("-", "").strip() == "" for cell in cells):
            continue

        remove_in = cells[2]
        if remove_in.startswith("`") and remove_in.endswith("`") and len(remove_in) >= 2:
            remove_in = remove_in[1:-1].strip()

        rows.append((cells[0], cells[1], remove_in))

    if not rows and not saw_none:
        add_error(errors, f"No deprecations found in {POLICY_PATH} table")

    return rows


def compare_registry_to_policy(errors, deprecations, rows):
    expected = {dep["item"]: (dep["replacement"], dep["remove_in"]) for dep in deprecations}
    found = {}

    for item, replacement, remove_in in rows:
        if item in found:
            add_error(errors, f"Duplicate item in policy table: {item}")
        found[item] = (replacement, remove_in)

    for item, (replacement, remove_in) in expected.items():
        if item not in found:
            add_error(errors, f"Policy table missing item: {item}")
            continue
        found_replacement, found_remove_in = found[item]
        if found_replacement != replacement:
            add_error(
                errors,
                f"Policy table replacement mismatch for '{item}': expected '{replacement}', got '{found_replacement}'",
            )
        if found_remove_in != remove_in:
            add_error(
                errors,
                f"Policy table remove_in mismatch for '{item}': expected '{remove_in}', got '{found_remove_in}'",
            )

    for item in found:
        if item not in expected:
            add_error(errors, f"Policy table has extra item not in registry: {item}")


def parse_constants(errors):
    if not CONFIG_PATH.exists():
        add_error(errors, f"Missing config constants: {CONFIG_PATH}")
        return {}

    text = CONFIG_PATH.read_text()
    constants = {}
    for match in re.finditer(r"(?m)^\s*([A-Za-z0-9_]+)\s*=\s*\"([^\"]+)\"", text):
        constants[match.group(1)] = match.group(2)

    return constants


def check_constants(errors, deprecations, constants):
    for dep in deprecations:
        const_name = dep.get("code_constant")
        if not const_name:
            continue
        if const_name not in constants:
            add_error(errors, f"Missing constant {const_name} in {CONFIG_PATH}")
            continue
        if constants[const_name] != dep["remove_in"]:
            add_error(
                errors,
                f"Constant {const_name} mismatch: expected {dep['remove_in']}, got {constants[const_name]}",
            )


def check_auth_log(errors, deprecations):
    if not AUTH_TOKEN_PATH.exists():
        add_error(errors, f"Missing auth token file: {AUTH_TOKEN_PATH}")
        return

    registry_ids = {dep.get("id") for dep in deprecations}
    if "query_token_auth" not in registry_ids:
        return

    text = AUTH_TOKEN_PATH.read_text()
    if "DeprecatedQueryTokenRemovalVersion" not in text:
        add_error(
            errors,
            f"{AUTH_TOKEN_PATH} should use DeprecatedQueryTokenRemovalVersion in deprecation logs",
        )


def check_deprecated_settings(errors, deprecations):
    if not CONFIG_DEPRECATED_SETTINGS_PATH.exists():
        # Non-fatal: the repo might restructure config files; keep other checks working.
        return

    registry_ids = {dep.get("id") for dep in deprecations}
    text = CONFIG_DEPRECATED_SETTINGS_PATH.read_text()
    for line in text.splitlines():
        if "DEPRECATED" not in line:
            continue
        match = DEPRECATED_ID_RE.search(line)
        if not match:
            add_error(errors, f"Deprecated setting missing deprecation_id: {line.strip()}")
            continue
        dep_id = match.group(1)
        if dep_id not in registry_ids:
            add_error(errors, f"Deprecated setting references unknown id: {dep_id}")


def main():
    errors = []

    deprecations = load_registry(errors)
    rows = parse_policy_table(errors)

    if deprecations and rows:
        compare_registry_to_policy(errors, deprecations, rows)
        constants = parse_constants(errors)
        check_constants(errors, deprecations, constants)

    check_auth_log(errors, deprecations)
    check_deprecated_settings(errors, deprecations)

    if errors:
        for error in errors:
            print(f"ERROR: {error}")
        sys.exit(1)

    print("Deprecation registry checks passed.")


if __name__ == "__main__":
    main()
