#!/bin/bash
# UI Contract Enforcement - Mechanical Gates (Scope-Aware)
# CTO Contract: These gates must pass before merging UI changes

set -e

# Script can be called with specific file patterns to check
# Usage: ./check-ui-contract.sh [file1] [file2] ...
# Example: ./check-ui-contract.sh Dashboard.tsx Dashboard.css ui/Card.tsx

# Determine project root (script is in scripts/)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
WEBUI_SRC="$PROJECT_ROOT/webui/src"

EXIT_CODE=0

# Parse scope arguments
SCOPE_FILES=("$@")
IS_SCOPED=false

if [ ${#SCOPE_FILES[@]} -gt 0 ]; then
  IS_SCOPED=true
  echo "==========================================  "
  echo "UI Contract Enforcement - SCOPED MODE"
  echo "Checking: ${SCOPE_FILES[*]}"
  echo "=========================================="
else
  echo "=========================================="
  echo "UI Contract Enforcement - FULL SCAN"
  echo "Checking: All files in webui/src"
  echo "=========================================="
fi
echo ""

cd "$WEBUI_SRC"

# Helper: build grep include pattern from scope
build_scope_pattern() {
  if [ "$IS_SCOPED" = true ]; then
    for file in "${SCOPE_FILES[@]}"; do
      echo "--include=$(basename "$file")"
    done
  else
    echo "."  # Full scan
  fi
}

# Gate 1: Token Compliance (Colors)
echo "Gate 1: Token Compliance"
echo "------------------------"
V1=""
if [ "$IS_SCOPED" = true ]; then
  for file in "${SCOPE_FILES[@]}"; do
    clean_file="${file#webui/src/}"
    result=$(grep -InE '#([0-9a-fA-F]{3,8})|rgb\(|rgba\(' "$clean_file" | grep -vE 'transparent|inherit|currentColor|index.css:.*--' || true)
    if [ -n "$result" ]; then
      V1="$V1$clean_file:$result"$'\n'
    fi
  done
else
  V1=$(grep -RInE '#([0-9a-fA-F]{3,8})|rgb\(|rgba\(' . \
    --include='*.css' --include='*.tsx' \
    --exclude-dir=node_modules \
    --exclude-dir=client-ts \
    | grep -vE 'transparent|inherit|currentColor|index.css:.*--' || true)
fi

if [ -z "$V1" ]; then
  echo "✅ PASS: No hardcoded hex colors"
else
  echo "❌ FAIL: Hardcoded colors found:"
  echo "$V1"
  EXIT_CODE=1
fi
echo ""

# Gate 2: Animation Budget (Scoped mode: FAIL on any animation in feature CSS)
echo "Gate 2: Animation Budget"
echo "------------------------"
V2=""
if [ "$IS_SCOPED" = true ]; then
  for file in "${SCOPE_FILES[@]}"; do
    if [[ "$file" == *.css ]]; then
      clean_file="${file#webui/src/}"
      result=$(grep -In "animation:" "$clean_file" || true)
      if [ -n "$result" ]; then
        V2="$V2$clean_file:$result"$'\n'
      fi
    fi
  done
else
  V2=$(grep -RIn 'animation: ' . \
    --include='*.css' \
    --exclude-dir=node_modules \
    | grep -vE "(pulse|index.css|StatusChip.css)" || true)
fi

if [ -z "$V2" ]; then
  echo "✅ PASS: Animation lifecycle compliant"
else
  echo "$([ "$IS_SCOPED" = true ] && echo "❌ FAIL" || echo "⚠️  WARNING"): Animation budget exceeded:"
  echo "$V2"
  [ "$IS_SCOPED" = true ] && EXIT_CODE=1
fi
echo ""

# Gate 3: Shadow Discipline
echo "Gate 3: Shadow Discipline"
echo "-------------------------"
V3=""
if [ "$IS_SCOPED" = true ]; then
  for file in "${SCOPE_FILES[@]}"; do
    if [[ "$file" == *.css ]]; then
      clean_file="${file#webui/src/}"
      result=$(grep -In "box-shadow:" "$clean_file" || true)
      if [ -n "$result" ]; then
        V3="$V3$clean_file:$result"$'\n'
      fi
    fi
  done
else
  V3=$(grep -RIn 'box-shadow: ' . \
    --include='*.css' \
    --exclude-dir=node_modules \
    | grep -vE "(index.css|Card.css|StatusChip.css|Navigation.css)" || true)
fi

if [ -z "$V3" ]; then
  echo "✅ PASS: No custom shadows in feature CSS"
else
  echo "$([ "$IS_SCOPED" = true ] && echo "❌ FAIL" || echo "⚠️  WARNING"): Custom shadows found:"
  echo "$V3"
  [ "$IS_SCOPED" = true ] && EXIT_CODE=1
fi
echo ""

# Gate 4: Gradient Discipline
echo "Gate 4: Gradient Discipline"
echo "---------------------------"
V4=""
if [ "$IS_SCOPED" = true ]; then
  for file in "${SCOPE_FILES[@]}"; do
    # Only check CSS files in scoped mode for gradients
    if [[ "$file" == *.css ]]; then
      clean_file="${file#webui/src/}"
      result=$(grep -InE "linear-gradient\(|radial-gradient\(" "$clean_file" || true)
      if [ -n "$result" ]; then
        V4="$V4$clean_file:$result"$'\n'
      fi
    fi
  done
else
  V4=$(grep -RInE 'linear-gradient\(|radial-gradient\(' . \
    --include='*.css' \
    --exclude-dir=node_modules \
    | grep -vE "index.css" || true)
fi

if [ -z "$V4" ]; then
  echo "✅ PASS: No hardcoded gradients in feature CSS"
else
  echo "$([ "$IS_SCOPED" = true ] && echo "❌ FAIL" || echo "⚠️  WARNING"): Custom gradients found:"
  echo "$V4"
  [ "$IS_SCOPED" = true ] && EXIT_CODE=1
fi
echo ""

# Gate 5: Inline Style Discipline
echo "Gate 5: Inline Style Discipline"
echo "--------------------------------"
V5=""
if [ "$IS_SCOPED" = true ]; then
  for file in "${SCOPE_FILES[@]}"; do
    if [[ "$file" == *.tsx ]]; then
      clean_file="${file#webui/src/}"
      
      # Step 1: Check for multi-line style props (FAIL immediately)
      multiline_check=$(grep -n "style={{" "$clean_file" | while read -r line; do
         if [[ ! "$line" =~ \}\} ]]; then
            echo "$line"
         fi
      done || true)
      
      if [ -n "$multiline_check" ]; then
         V5="$V5$clean_file: (BLOCK) Multi-line style={{}} detected. Use one-liner or CSS variables."$'\n'"$multiline_check"$'\n'
      fi

      # Step 2: Validate one-liners
      result=$(grep -In 'style={{' "$clean_file" | while read -r line; do
        # Extract content between {{ and }}
        content=$(echo "$line" | sed 's/.*style={{//; s/}}.*//' | sed 's/ as .*//')
        
        # We need a per-line status check here without global race
        line_violation=false
        
        # ADR-00X: Strict one-liner check. Custom properties only.
        # Strip permitted custom properties to see if any forbidden styles remain.
        # We handle both quoted and unquoted keys.
        remaining=$(echo "$content" | sed -E "s/['\"]?--[a-z0-9-]*['\"]?:[^,}]*//g" | tr -d ' ')
        
        if [[ "$remaining" =~ ":" ]]; then
           line_violation=true
        fi
        
        if [ "$line_violation" = true ]; then
          echo "$line"
        fi
      done || true)

      if [ -n "$result" ]; then
        V5="$V5$clean_file:$result"$'\n'
      fi
    fi
  done
else
  V5=$(grep -RIn 'style={{' . \
    --include='*.tsx' \
    --exclude-dir=node_modules \
    | grep -vE "(--xg2g-|--v3-)" || true)
fi

if [ -z "$V5" ]; then
  echo "✅ PASS: No illegal inline styles"
else
  echo "$([ "$IS_SCOPED" = true ] && echo "❌ FAIL" || echo "⚠️  WARNING"): Illegal style={{}} found:"
  echo "$V5"
  echo "(Refactored files allow only --xg2g-* or --v3-* variable passthrough)"
  [ "$IS_SCOPED" = true ] && EXIT_CODE=1
fi
echo ""
echo ""

# Summary
echo "=========================================="
if [ $EXIT_CODE -eq 0 ]; then
  echo "✅ ALL GATES PASSED - UI Contract Enforced"
  echo "=========================================="
else
  echo "❌ GATES FAILED - Fix violations before merge"
  echo "=========================================="
fi

exit $EXIT_CODE
