#!/usr/bin/env python3
import yaml
import sys

class DuplicateKeyError(Exception):
    pass

def dict_constructor(loader, node, deep=False):
    mapping = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node, deep=deep)
        if key in mapping:
            line_num = key_node.start_mark.line + 1
            print(f"❌ Duplicate key found: '{key}' at line {line_num}")
            sys.exit(1)
        value = loader.construct_object(value_node, deep=deep)
        mapping[key] = value
    return mapping

yaml.SafeLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, dict_constructor)

def check_duplicates(path):
    with open(path, 'r') as f:
        try:
            yaml.load(f, Loader=yaml.SafeLoader)
            print("✅ No duplicate mapping keys found.")
        except SystemExit:
            sys.exit(1)
        except Exception as e:
            print(f"Error parsing YAML: {e}")
            sys.exit(1)

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: verify-openapi-no-duplicate-keys.py <path>")
        sys.exit(1)
    check_duplicates(sys.argv[1])
