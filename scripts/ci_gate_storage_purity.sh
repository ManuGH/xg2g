#!/bin/bash
# scripts/ci_gate_storage_purity.sh
# Purpose: Enforces "Single Durable Truth" by forbidding legacy store drivers
# in shipping production code, while allowing them for migration tooling.
# Per ADR-021: BoltDB/BadgerDB Sunset Policy and Enforcement

set -e

FORBIDDEN_IMPORTS=(
    "go.etcd.io/bbolt"
    "github.com/dgraph-io/badger/v4"
)

# Exception paths (allowed to import legacy stores for migration/tooling)
# Per ADR-021: Only migration tooling may import bolt/badger
# These paths will be removed in Phase 3.0 (Q2 2026)
EXCEPTIONS=(
    "internal/migration/"          # Migration read logic
    "cmd/xg2g-migrate/"            # Migration CLI tool
    "internal/domain/session/store/bolt_store.go"    # Legacy store (unreachable via factory.go, removed Phase 2.4)
    "internal/domain/session/store/badger_store.go"  # Legacy store (unreachable via factory.go, removed Phase 2.4)
    "internal/pipeline/resume/store.go"              # Temporary: still has bolt import (cleaned Phase 2.4)
)

echo "🔍 Checking Storage Purity (No forbidden durable stores in production)..."

VIOLATIONS=0
for PKG in "${FORBIDDEN_IMPORTS[@]}"; do
    # Find all .go files (excluding tests) that import forbidden packages
    FOUND=$(grep -r "$PKG" internal/ cmd/ --include="*.go" --exclude="*_test.go" || true)

    if [ -z "$FOUND" ]; then
        continue
    fi

    # Filter out exceptions
    FILTERED=""
    while IFS= read -r line; do
        if [ -z "$line" ]; then
            continue
        fi

        # Extract file path from grep output (format: "path/file.go:content")
        FILE_PATH=$(echo "$line" | cut -d':' -f1)

        # Check if file matches any exception
        IS_EXEMPT=false
        for EXC in "${EXCEPTIONS[@]}"; do
            if [[ "$FILE_PATH" == *"$EXC"* ]]; then
                IS_EXEMPT=true
                break
            fi
        done

        # If not exempt, add to violations
        if [ "$IS_EXEMPT" = false ]; then
            if [ -z "$FILTERED" ]; then
                FILTERED="$line"
            else
                FILTERED="$FILTERED
$line"
            fi
        fi
    done <<< "$FOUND"

    if [ -n "$FILTERED" ]; then
        echo "❌ VIOLATION: Forbidden import '$PKG' found in production code:"
        echo "$FILTERED"
        VIOLATIONS=$((VIOLATIONS + 1))
    fi
done

if [ "$VIOLATIONS" -gt 0 ]; then
    echo "--------------------------------------------------------"
    echo "🚨 FAIL: Storage purity check failed (ADR-021 violation)."
    echo "💡 Only SQLite (modernc.org/sqlite) is allowed for durable state."
    echo "💡 Production code MUST NOT import Bolt or Badger."
    echo "💡 See docs/ADR/021-boltdb-sunset-enforcement.md for policy."
    echo "--------------------------------------------------------"
    exit 1
fi

echo "✅ PASS: Storage purity verified (ADR-021 compliant)."
exit 0
