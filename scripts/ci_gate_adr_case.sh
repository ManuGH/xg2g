#!/bin/bash
# scripts/ci_gate_adr_case.sh
# Purpose: Enforces correct ADR path case sensitivity (docs/ADR/ not docs/adr/)
# Rationale: Linux/Docker are case-sensitive; lowercase paths break CI and developer experience.

set -e

echo "🔍 Verifying ADR Path Case Sensitivity..."

# Search for any reference to lowercase docs/adr/
# Exclude binary files, git directory, and the gate script itself
VIOLATIONS=$(rg -n --type-not binary "docs/adr/" \
    --glob '!.git/**' \
    --glob '!scripts/ci_gate_adr_case.sh' \
    2>/dev/null || true)

if [ -n "$VIOLATIONS" ]; then
    echo "❌ FAIL: Found lowercase docs/adr/ references (must be docs/ADR/)"
    echo ""
    echo "Violations:"
    echo "$VIOLATIONS"
    echo ""
    echo "--------------------------------------------------------"
    echo "🚨 STOP THE LINE 🚨"
    echo "All ADR references MUST use uppercase: docs/ADR/"
    echo ""
    echo "Why: Linux and Docker use case-sensitive filesystems."
    echo "     Lowercase paths break CI and cause broken links."
    echo ""
    echo "Fix: Replace all 'docs/adr/' with 'docs/ADR/'"
    echo "--------------------------------------------------------"
    exit 1
fi

echo "✅ PASS: All ADR paths use correct case (docs/ADR/)"
exit 0
