#!/usr/bin/env python3
import yaml
import sys
import os
import json
from datetime import datetime

class DuplicateKeyError(Exception):
    pass

def dict_constructor(loader, node, deep=False):
    mapping = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node, deep=deep)
        if key in mapping:
            line_num = key_node.start_mark.line + 1
            print(f"❌ YAML Error: Duplicate key '{key}' found at line {line_num}. Critical for contract governance.")
            sys.exit(5)
        value = loader.construct_object(value_node, deep=deep)
        mapping[key] = value
    return mapping

yaml.SafeLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, dict_constructor)

def find_refs(node, refs):
    if isinstance(node, dict):
        if "$ref" in node:
            ref = node["$ref"]
            if ref.startswith("#/components/schemas/"):
                schema_name = ref.split("/")[-1]
                if schema_name not in refs:
                    refs.add(schema_name)
                    return True # New ref found
        else:
            found_new = False
            for val in node.values():
                if find_refs(val, refs):
                    found_new = True
            return found_new
    elif isinstance(node, list):
        found_new = False
        for item in node:
            if find_refs(item, refs):
                found_new = True
        return found_new
    return False

def get_v3_schemas(openapi_path):
    with open(openapi_path, 'r') as f:
        spec = yaml.load(f, Loader=yaml.SafeLoader)

    v3_schemas = set()
    
    # Traverse all paths in openapi.yaml (v3 dedicated spec)
    paths = spec.get("paths", {})
    for path, methods in paths.items():
        find_refs(methods, v3_schemas)

    # Recursively find transitive refs within the schemas themselves
    all_schemas = spec.get("components", {}).get("schemas", {})
    
    changed = True
    while changed:
        changed = False
        current_refs = list(v3_schemas)
        for schema_name in current_refs:
            schema_node = all_schemas.get(schema_name, {})
            if find_refs(schema_node, v3_schemas):
                changed = True
                
    return v3_schemas, all_schemas

def check_hygiene(openapi_path, allowlist_path=None):
    allowlist = {}
    if allowlist_path and os.path.exists(allowlist_path):
        with open(allowlist_path, 'r') as f:
            allowlist_data = json.load(f)
            for item in allowlist_data:
                name = item.get("name")
                if not name or not item.get("reason") or not item.get("adr_link") or not item.get("expiry"):
                    print(f"❌ Malformed allowlist entry: {item}")
                    return 2
                
                expiry_str = item.get("expiry")
                if expiry_str != "never":
                    expiry_date = datetime.strptime(expiry_str, "%Y-%m-%d")
                    if expiry_date < datetime.now():
                        print(f"❌ Expired allowlist entry for '{name}': {expiry_str}")
                        return 3
                allowlist[name] = item

    v3_schemas, all_schemas = get_v3_schemas(openapi_path)
    
    scope_output = ["--- OpenAPI V3 Scoped Schemas (Audit) ---"]
    for s in sorted(v3_schemas):
        scope_output.append(f"  - {s}")
    scope_output.append("-----------------------------------------")

    exit_code = 0
    violations = []
    
    for schema_name in sorted(v3_schemas):
        if schema_name in allowlist:
            continue

        schema = all_schemas.get(schema_name, {})
        props = schema.get("properties", {})
        
        for prop_name in props:
            if "_" in prop_name:
                violations.append(f"❌ Hygiene Violation: Schema '{schema_name}' has property '{prop_name}' with underscores. Use camelCase.")
                exit_code = 1
                
        if any(keyword in schema_name for keyword in ["Playback", "Problem", "Decision", "Trace"]):
            if "properties" in schema and not schema.get("additionalProperties") is False:
                 violations.append(f"❌ Hygiene Violation: Schema '{schema_name}' is missing 'additionalProperties: false'.")
                 exit_code = 1

    if exit_code != 0:
        print("\n".join(scope_output))
        print("\n".join(violations))
    else:
        print("✅ OpenAPI V3 Hygiene verified (No Casing/Structure Violations).")
    
    return exit_code

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: openapi_v3_scope.py <openapi_path> [allowlist_json]")
        sys.exit(1)
    
    allowlist_path = sys.argv[2] if len(sys.argv) > 2 else None
    sys.exit(check_hygiene(sys.argv[1], allowlist_path))
