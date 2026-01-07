#!/usr/bin/env python3
import os
import re
import sys
from pathlib import Path

# Simple link checker for local Markdown files
# Usage: python3 tools/check_links.py [directory]

ROOT_DIR = Path(".").resolve()
DOCS_DIR = ROOT_DIR / "docs"
README_FILE = ROOT_DIR / "README.md"

LINK_REGEX = re.compile(r'\[([^\]]+)\]\(([^)]+)\)')

def check_file(file_path):
    errors = []
    with open(file_path, 'r', encoding='utf-8') as f:
        content = f.read()
    
    matches = LINK_REGEX.findall(content)
    for text, link in matches:
        # Skip external links
        if link.startswith('http') or link.startswith('mailto:'):
            continue
        
        # Skip weird anchors for now (unless simple #)
        if link.startswith('#'):
            # TODO: check internal anchors
            continue
            
        # Clean link (remove query/fragment)
        clean_link = link.split('#')[0].split('?')[0]
        if not clean_link:
            continue

        # Resolve path
        if clean_link.startswith('/'):
            # Absolute path relative to repo root (assumption)
            target = ROOT_DIR / clean_link.lstrip('/')
        else:
            # Relative path
            target = (file_path.parent / clean_link).resolve()
        
        if not target.exists():
            errors.append(f"Broken link: '{link}' -> '{target}' (not found)")
            
    return errors

def main():
    print("Checking documentation links...")
    files_to_check = list(DOCS_DIR.rglob("*.md"))
    if README_FILE.exists():
        files_to_check.append(README_FILE)
    
    total_errors = 0
    for file_path in files_to_check:
        # Skip archive
        if "_archive" in str(file_path):
            continue
            
        errors = check_file(file_path)
        if errors:
            print(f"\n❌ {file_path.relative_to(ROOT_DIR)}:")
            for e in errors:
                print(f"  - {e}")
                total_errors += 1
                
    if total_errors > 0:
        print(f"\n💥 Found {total_errors} broken links.")
        sys.exit(1)
    else:
        print("\n✅ All links checked. No issues found.")

if __name__ == "__main__":
    main()
