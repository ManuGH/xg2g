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

# Zero exceptions allowed as of v3.0.0 (Phase 3.0 Sunset complete)
EXCEPTIONS=()

# 0. Check for exceptions creep (Phase 3.0 enforcement: ZERO EXCEPTIONS)
if [ "${#EXCEPTIONS[@]}" -gt 0 ]; then
    echo "🚨 FAIL: Forbidden exceptions found: '${EXCEPTIONS[*]}'. Phase 3.0 requires zero exceptions."
    exit 1
fi

echo "🔍 Checking Storage Purity (No forbidden durable stores in production)..."

VIOLATIONS=0

# 1. Check for Forbidden Imports (bolt/badger)
for PKG in "${FORBIDDEN_IMPORTS[@]}"; do
    # Find all .go files (excluding tests) that import forbidden packages
    # Explicitly targeting internal/ and cmd/ to ignore docs/ADR
    FOUND=$(grep -r "$PKG" internal/ cmd/ --include="*.go" --exclude="*_test.go" || true)

    if [ -n "$FOUND" ]; then
        while IFS= read -r line; do
            if [ -z "$line" ]; then continue; fi
            FILE_PATH=$(echo "$line" | cut -d':' -f1)
            echo "❌ VIOLATION: Forbidden import '$PKG' found in: $FILE_PATH"
            VIOLATIONS=$((VIOLATIONS + 1))
        done <<< "$FOUND"
    fi
done

# 2. Check for Pattern-based markers (JsonStore, NewJsonStore, etc.)
MARKERS=(
    "JsonStore"
    "NewJsonStore"
    "BoltStore"
    "NewBoltStore"
    "BadgerStore"
    "NewBadgerStore"
)

for MARKER in "${MARKERS[@]}"; do
    FOUND=$(grep -r "$MARKER" internal/ cmd/ --include="*.go" --exclude="*_test.go" || true)
    
    if [ -n "$FOUND" ]; then
        while IFS= read -r line; do
            if [ -z "$line" ]; then continue; fi
            FILE_PATH=$(echo "$line" | cut -d':' -f1)
            echo "❌ VIOLATION: Legacy store marker '$MARKER' found in: $FILE_PATH"
            VIOLATIONS=$((VIOLATIONS + 1))
        done <<< "$FOUND"
    fi
done

# 3. Check for String-based backend selectors in code
FORBIDDEN_STRINGS=("bolt" "badger" "json")
for STR in "${FORBIDDEN_STRINGS[@]}"; do
    # Regex to find these as distinct words/values to avoid catching "encoding/json" etc.
    # We scan internal/ and cmd/ but exclude tests, docs, ADRs, and DEPRECATION_STATUS.md
    FOUND=$(grep -rE "([\"']$STR[\"']\s*(,|;|\)|$)|backend\s*[:=]\s*[\"']$STR[\"']|backend\s*==\s*[\"']$STR[\"'])" internal/ cmd/ \
        --include="*.go" \
        --exclude="*_test.go" || true)
    
    if [ -n "$FOUND" ]; then
        while IFS= read -r line; do
            if [ -z "$line" ]; then continue; fi
            FILE_PATH=$(echo "$line" | cut -d':' -f1)
            
            # Double check: factory errors are ALLOWED if they just return the error string
            # and contain the ADR-021 rejection marker.
            if echo "$line" | grep -qiE "(ADR-021|DEPRECATED|removed|supported:)"; then
                continue
            fi

            # False positive protection for ffprobe/ffmpeg JSON output
            if echo "$line" | grep -qE "(\"-print_format\"|\"json\"|\"format\")"; then
                if echo "$line" | grep -qE "(\"-print_format\"|\"-f\"|\"-of\")"; then
                    continue
                fi
            fi

            # False positive protection for CLI flags
            if echo "$line" | grep -q "Flags()."; then
                continue
            fi

            echo "❌ VIOLATION: Legacy backend string '$STR' found in production code: $line"
            VIOLATIONS=$((VIOLATIONS + 1))
        done <<< "$FOUND"
    fi
done

if [ "$VIOLATIONS" -gt 0 ]; then
    echo "--------------------------------------------------------"
    echo "🚨 FAIL: Storage purity check failed (ADR-021 violation)."
    echo "💡 Only SQLite (modernc.org/sqlite) is allowed for durable state."
    echo "💡 Production code MUST NOT implement or select Bolt/Badger/JSON."
    echo "💡 See docs/ADR/021-boltdb-sunset-enforcement.md for policy."
    echo "--------------------------------------------------------"
    exit 1
fi

echo "✅ PASS: Storage purity verified (ADR-021 compliant)."
exit 0
