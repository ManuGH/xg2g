#!/bin/bash
set -e
mkdir -p /tmp/xg2g-audit

echo "--- Backend Inventory ---"
# A) All Test Files
git ls-files '*_test.go' | sort > /tmp/xg2g-audit/tests_go_files.txt

# B) Packages with Tests
go list ./... > /tmp/xg2g-audit/go_packages.txt

# C) Test Names per Package
(go list ./... | while read -r pkg; do
  tests=$(go test -list '^Test' "$pkg" 2>/dev/null | grep '^Test' || true)
  if [ -n "$tests" ]; then
    echo "=== $pkg ==="
    echo "$tests"
    echo
  fi
done) > /tmp/xg2g-audit/go_test_inventory.txt

# D) Count Tests per Package
(go list ./... | while read -r pkg; do
  n=$(go test -list '^Test' "$pkg" 2>/dev/null | grep -c '^Test' || true)
  if [ "$n" -gt 0 ]; then
    printf "%4d  %s\n" "$n" "$pkg"
  fi
done | sort -nr) > /tmp/xg2g-audit/go_test_counts.txt

echo "--- Frontend Inventory ---"
# A) Frontend Test Files
git ls-files 'webui/**/*.(test|spec).(ts|tsx|js|jsx)' | sort > /tmp/xg2g-audit/tests_webui_files.txt

# B) Policy Regression / Checks (grep)
rg -n "describe\(|it\(|test\(" webui/src | head -n 200 > /tmp/xg2g-audit/webui_test_grep.txt || true

echo "--- CI / Scripts Inventory ---"
# A) Workflow Files
git ls-files '.github/workflows/*' > /tmp/xg2g-audit/ci_workflows_files.txt

# B) Verify/Lint Scripts
git ls-files | rg -n "(verify|check|lint|audit).*\.sh$" > /tmp/xg2g-audit/verify_scripts.txt || true

echo "✅ Inventory generated in /tmp/xg2g-audit/"
ls -l /tmp/xg2g-audit/
