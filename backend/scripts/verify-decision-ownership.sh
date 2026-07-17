#!/bin/bash
# verify-decision-ownership.sh
# Enforces the migration boundary between the authoritative live planner and
# the legacy recording decision package.

set -euo pipefail

DECISION_PKG="github.com/ManuGH/xg2g/internal/control/recordings/decision"
LEGACY_RUNTIME_OWNER="internal/control/http/v3/recordings/playback_info.go"

# Exact production imports that still exchange legacy decision DTOs, provide
# offline audit/reporting, or wire the non-live compatibility path. An entry is
# removed as soon as that adapter migrates. The migration exit condition is an
# empty list plus deletion of LEGACY_RUNTIME_OWNER when recording playback also
# uses playbackplanner.
PRODUCTION_IMPORT_ALLOWLIST=(
    "cmd/daemon/storage_decision_report_render.go"
    "cmd/daemon/storage_decision_report_sqlite.go"
    "cmd/daemon/storage_decision_sweep_clients.go"
    "cmd/daemon/storage_decision_sweep_cmd.go"
    "cmd/daemon/storage_decision_sweep_deps.go"
    "cmd/daemon/storage_decision_sweep_diff.go"
    "internal/app/bootstrap/bootstrap.go"
    "internal/control/http/v3/intents/start_profile_policy.go"
    "internal/control/http/v3/playback_info_http_adapter.go"
    "internal/control/http/v3/playback_info_response_mapping.go"
    "internal/control/http/v3/playback_info_runtime_state.go"
    "internal/control/http/v3/recordings/capability_registry.go"
    "internal/control/http/v3/recordings/deps.go"
    "$LEGACY_RUNTIME_OWNER"
    "internal/control/http/v3/recordings/playback_info_types.go"
    "internal/control/http/v3/recordings/playback_transport_policy.go"
    "internal/control/http/v3/server.go"
    "internal/control/playbackshadow/shadow.go"
    "internal/health/lifecycle_upgrade.go"
)

is_test_surface() {
    local file="$1"
    [[ "$file" == *_test.go || "$file" == test/* ]]
}

is_production_import_allowlisted() {
    local file="$1"
    local allowed
    for allowed in "${PRODUCTION_IMPORT_ALLOWLIST[@]}"; do
        if [[ "$file" == "$allowed" ]]; then
            return 0
        fi
    done
    return 1
}

echo "--- Checking Decision Ownership (Planner Cutover Boundary) ---"

# Fail if the migration inventory silently goes stale through a rename. This
# keeps every production exception explicit and reviewable.
for file in "${PRODUCTION_IMPORT_ALLOWLIST[@]}"; do
    if [[ ! -f "$file" ]]; then
        echo "❌ FAIL: allowlisted migration surface is missing: $file"
        exit 1
    fi
done

hits_total=0
hits_allowed=0
hits_actionable=0

while IFS= read -r file; do
    [[ -z "$file" ]] && continue
    hits_total=$((hits_total + 1))

    if is_test_surface "$file" || is_production_import_allowlisted "$file"; then
        hits_allowed=$((hits_allowed + 1))
    else
        hits_actionable=$((hits_actionable + 1))
        echo "❌ IMPORT_VIOLATION: $file imports the legacy decision package"
    fi
done < <(git grep -l -F "\"$DECISION_PKG\"" -- '*.go' || true)

# Tests may characterize either engine. Production may invoke the legacy
# resolver only in the single non-live compatibility owner. In particular,
# audit/reporting and DTO adapters are allowed to import legacy types but may
# not grow another decision machine.
while IFS= read -r match; do
    [[ -z "$match" ]] && continue
    file="${match%%:*}"
    if is_test_surface "$file"; then
        continue
    fi
    if [[ "$file" != "$LEGACY_RUNTIME_OWNER" ]]; then
        hits_actionable=$((hits_actionable + 1))
        echo "❌ CALL_VIOLATION: $match"
    fi
done < <(git grep -n -E '(^|[^[:alnum:]_])decision\.Decide\(' -- '*.go' || true)

# Guard the one remaining production call structurally: the live branch must
# occur first and return before execution can reach the legacy resolver. This
# closes the otherwise dangerous loophole of moving a live call into the
# allowlisted compatibility owner.
legacy_call_count=$(grep -c 'decision\.Decide(' "$LEGACY_RUNTIME_OWNER" || true)
live_guard_line=$(grep -nF 'if req.SchemaType == "live" {' "$LEGACY_RUNTIME_OWNER" | head -1 | cut -d: -f1 || true)
legacy_call_line=$(grep -n 'decision\.Decide(' "$LEGACY_RUNTIME_OWNER" | head -1 | cut -d: -f1 || true)
if [[ "$legacy_call_count" != "1" || -z "$live_guard_line" || -z "$legacy_call_line" || "$live_guard_line" -ge "$legacy_call_line" ]]; then
    hits_actionable=$((hits_actionable + 1))
    echo "❌ LIVE_GUARD_VIOLATION: expected one legacy call after the authoritative live branch"
else
    live_return_count=$(sed -n "$((live_guard_line + 1)),$((legacy_call_line - 1))p" "$LEGACY_RUNTIME_OWNER" | grep -c 'return PlaybackInfoResult{' || true)
    if [[ "$live_return_count" -lt 1 ]]; then
        hits_actionable=$((hits_actionable + 1))
        echo "❌ LIVE_GUARD_VIOLATION: live branch does not return before decision.Decide"
    fi
fi

# The live handoff must never regain the old runtime referee. Comparisons stay
# inside playbackshadow and tests; receipt issuance is planner-only.
while IFS= read -r match; do
    [[ -z "$match" ]] && continue
    file="${match%%:*}"
    if is_test_surface "$file"; then
        continue
    fi
    hits_actionable=$((hits_actionable + 1))
    echo "❌ RECEIPT_GATE_VIOLATION: $match"
done < <(git grep -n -E '(^|[^[:alnum:]_])IssueEquivalent\(' -- 'internal/control/http/v3/*.go' 'internal/control/http/v3/**/*.go' || true)

echo ""
echo "Summary:"
echo "  LEGACY_IMPORTS_TOTAL=$hits_total"
echo "  LEGACY_IMPORTS_ALLOWLISTED=$hits_allowed"
echo "  ACTIONABLE_VIOLATIONS=$hits_actionable"

if [[ $hits_actionable -ne 0 ]]; then
    echo "❌ FAIL: decision ownership boundary violated"
    exit 1
fi

echo "✅ PASS: live planner ownership and legacy migration boundary verified"
