#!/bin/bash
# scripts/ci_gate_storage_purity.sh
# Purpose: Enforces "Single Durable Truth" by forbidding legacy store drivers
# in shipping production code, while allowing them for migration tooling.

set -e

FORBIDDEN_IMPORTS=(
    "go.etcd.io/bbolt"
    "github.com/dgraph-io/badger/v4"
)

# Exception paths (allowed to import legacy stores for migration/tooling)
EXCEPTIONS=(
    "internal/tools"
    "cmd/xg2g-migrate"
)

echo "🔍 Checking Storage Purity (No forbidden durable stores in production)..."

VIOLATIONS=0
for PKG in "${FORBIDDEN_IMPORTS[@]}"; do
    # Search in internal/ and cmd/
    # Exclude tests and migration tooling
    GREP_EXCLUDES="--exclude=*_test.go"
    for EXC in "${EXCEPTIONS[@]}"; do
        GREP_EXCLUDES="$GREP_EXCLUDES --exclude-dir=$EXC"
    done

    FOUND=$(grep -r "$PKG" internal/ cmd/ $GREP_EXCLUDES --include="*.go" || true)
    
    if [ -n "$FOUND" ]; then
        echo "❌ VIOLATION: Forbidden import '$PKG' found in shipping code:"
        echo "$FOUND"
        VIOLATIONS=$((VIOLATIONS + 1))
    fi
done

if [ "$VIOLATIONS" -gt 0 ]; then
    echo "--------------------------------------------------------"
    echo "🚨 FAIL: Storage purity check failed."
    echo "💡 Only SQLite (modernc.org/sqlite) is allowed for durable state."
    echo "💡 Shipping code MUST NOT import Bolt or Badger."
    echo "--------------------------------------------------------"
    exit 1
fi

echo "✅ PASS: Storage purity verified."
exit 0
