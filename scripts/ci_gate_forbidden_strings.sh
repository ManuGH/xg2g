#!/bin/bash
# scripts/ci_gate_forbidden_strings.sh
# Fails if specific forbidden strings are found in the codebase.

FORBIDDEN="strings.Contains.*deadline exceeded"
echo "Checking for forbidden patterns: $FORBIDDEN"

if grep -rnE "$FORBIDDEN" internal/control/http/v3/recordings; then
  echo "FAIL: Forbidden string matching detected."
  exit 1
fi
echo "PASS: No forbidden string matching found."
