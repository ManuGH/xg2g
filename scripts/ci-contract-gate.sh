#!/bin/bash
set -e

echo "=== V3 WebUI Contract Gate ==="

# 1. Grep for 'as any' usage in strict source files (excluding tests/generated)
echo "Checking for illegal 'as any' casts..."
if grep -r "as any" webui/src --include="*.tsx" --include="*.ts" --exclude-dir="client-ts" --exclude-dir="node_modules" --exclude="*.test.tsx" --exclude="*.spec.tsx" --exclude="*.test.ts"; then
    echo "❌ Usage of 'as any' detected! This violates strict contract safety."
    exit 1
fi
echo "✅ No 'as any' found in source code."

# 2. Grep for legacy .ref usage on Service objects (removed in v3.1)
echo "Checking for legacy '.ref' service access..."
# Use word boundary and ensure it's a property access (preceded by something that isn't just a start of line if possible, though .ref usually implies object.ref)
# We exclude 'useRef' explicitly, but the regex below should handle most cases.
# Pattern: (anything)\.ref(word-boundary)
if grep -rE "\.ref\b" webui/src --include="*.tsx" --include="*.ts" --exclude-dir="client-ts" --exclude-dir="node_modules" | grep -v "useRef"; then
    echo "❌ Usage of legacy '.ref' detected! Use '.service_ref' or '.id' instead."
    exit 1
fi
echo "✅ No legacy '.ref' usage found."

# 3. Verify no raw bouquet string arrays (heuristic: Array<string> in bouquet context)
# This is harder to grep, relying on type-check primarily. 
# But let's check for the old map pattern: "bouquets: string[]"
echo "Checking for legacy bouquet string arrays..."
if grep -r "bouquets: string\[\]" webui/src/features/epg; then
    echo "❌ EPG feature must not use 'bouquets: string[]'. Use 'EpgBouquet[]'."
    exit 1
fi
echo "✅ No legacy bouquet types in EPG."

echo "=== Contract Gate Passed ==="
exit 0
