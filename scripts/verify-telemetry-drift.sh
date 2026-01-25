#!/bin/bash
set -e

# verify-telemetry-drift.sh
# Ensures contracts/telemetry.schema.json matches contracts/telemetry.snapshot.json
# Telemetry signals are normative contracts with the operator/dashboard. They cannot change implicitly.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCHEMA_FILE="$REPO_ROOT/contracts/telemetry.schema.json"
SNAPSHOT_FILE="$REPO_ROOT/contracts/telemetry.snapshot.json"

echo "--- Verifying Telemetry Drift ---"

if ! cmp -s "$SCHEMA_FILE" "$SNAPSHOT_FILE"; then
    echo "❌ Drift detected! 'telemetry.schema.json' does not match 'telemetry.snapshot.json'."
    echo "   Rationale: Changes to Operational Telemetry MUST be explicitly committed to snapshot."
    echo "   Fix: cp contracts/telemetry.schema.json contracts/telemetry.snapshot.json"
    exit 1
fi

echo "✅ Telemetry snapshot is up-to-date."
