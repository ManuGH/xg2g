#!/bin/bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEFAULT_DB="$REPO_ROOT/build/dev-store/sessions.sqlite"
DB_PATH="${1:-$DEFAULT_DB}"
SHORT_THRESHOLD_MS="${SHORT_THRESHOLD_MS:-30000}"

if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "sqlite3 not found in PATH" >&2
  exit 1
fi

if [[ ! -f "$DB_PATH" ]]; then
  echo "session store not found: $DB_PATH" >&2
  exit 1
fi

BASE_CTE="
WITH base AS (
  SELECT
    session_id,
    service_ref,
    state,
    reason,
    created_at_ms,
    updated_at_ms,
    COALESCE(json_extract(profile_json, '\$.name'), '') AS profile_name,
    COALESCE(json_extract(playback_trace_json, '\$.clientPath'), '') AS client_path,
    COALESCE(json_extract(playback_trace_json, '\$.policyModeHint'), '') AS policy_mode_hint,
    COALESCE(json_extract(playback_trace_json, '\$.effectiveRuntimeMode'), '') AS effective_runtime_mode,
    COALESCE(json_extract(playback_trace_json, '\$.effectiveModeSource'), '') AS effective_mode_source,
    COALESCE(json_extract(playback_trace_json, '\$.stopReason'), '') AS trace_stop_reason,
    COALESCE(json_extract(playback_trace_json, '\$.targetProfileHash'), '') AS target_profile_hash
  FROM sessions
)
"

run_query() {
  local title="$1"
  local sql="$2"
  echo
  echo "=== $title ==="
  sqlite3 -header -column "$DB_PATH" "$BASE_CTE $sql"
}

echo "Runtime mode drift snapshot"
echo "db: $DB_PATH"
echo "short threshold: ${SHORT_THRESHOLD_MS}ms"

run_query "Coverage" "
SELECT
  COUNT(*) AS total_sessions,
  SUM(CASE WHEN playback_trace_json IS NOT NULL AND playback_trace_json <> '' THEN 1 ELSE 0 END) AS traced_sessions,
  SUM(CASE WHEN policy_mode_hint <> '' AND effective_runtime_mode <> '' AND effective_mode_source <> '' THEN 1 ELSE 0 END) AS instrumented_sessions,
  SUM(CASE WHEN policy_mode_hint = '' OR effective_runtime_mode = '' OR effective_mode_source = '' THEN 1 ELSE 0 END) AS rows_missing_runtime_fields
FROM (
  SELECT
    s.playback_trace_json,
    b.policy_mode_hint,
    b.effective_runtime_mode,
    b.effective_mode_source
  FROM sessions s
  JOIN base b USING (session_id)
);
"

run_query "EffectiveModeSource Distribution" "
SELECT
  effective_mode_source,
  COUNT(*) AS sessions
FROM base
WHERE effective_mode_source <> ''
GROUP BY effective_mode_source
ORDER BY sessions DESC, effective_mode_source ASC;
"

run_query "Hint vs Effective Drift" "
SELECT
  service_ref,
  CASE WHEN client_path = '' THEN '(none)' ELSE client_path END AS client_path,
  policy_mode_hint,
  effective_runtime_mode,
  effective_mode_source,
  COUNT(*) AS sessions
FROM base
WHERE policy_mode_hint <> ''
  AND effective_runtime_mode <> ''
  AND policy_mode_hint <> effective_runtime_mode
GROUP BY service_ref, client_path, policy_mode_hint, effective_runtime_mode, effective_mode_source
ORDER BY sessions DESC, service_ref ASC, client_path ASC
LIMIT 20;
"

run_query "Rows With Unknown / Missing Runtime Fields" "
SELECT
  session_id,
  state,
  service_ref,
  profile_name,
  CASE WHEN client_path = '' THEN '(none)' ELSE client_path END AS client_path,
  CASE WHEN policy_mode_hint = '' THEN '(missing)' ELSE policy_mode_hint END AS policy_mode_hint,
  CASE WHEN effective_runtime_mode = '' THEN '(missing)' ELSE effective_runtime_mode END AS effective_runtime_mode,
  CASE WHEN effective_mode_source = '' THEN '(missing)' ELSE effective_mode_source END AS effective_mode_source,
  datetime(created_at_ms / 1000, 'unixepoch', 'localtime') AS created_local
FROM base
WHERE policy_mode_hint = ''
   OR effective_runtime_mode = ''
   OR effective_mode_source = ''
ORDER BY created_at_ms DESC
LIMIT 20;
"

run_query "Short-Lived Drift Summary" "
SELECT
  service_ref,
  policy_mode_hint,
  effective_runtime_mode,
  effective_mode_source,
  COUNT(*) AS sessions,
  SUM(CASE WHEN (updated_at_ms - created_at_ms) < $SHORT_THRESHOLD_MS THEN 1 ELSE 0 END) AS short_sessions,
  ROUND(AVG((updated_at_ms - created_at_ms) / 1000.0), 1) AS avg_duration_s,
  ROUND(MIN((updated_at_ms - created_at_ms) / 1000.0), 1) AS min_duration_s,
  ROUND(MAX((updated_at_ms - created_at_ms) / 1000.0), 1) AS max_duration_s
FROM base
WHERE policy_mode_hint <> ''
  AND effective_runtime_mode <> ''
  AND policy_mode_hint <> effective_runtime_mode
GROUP BY service_ref, policy_mode_hint, effective_runtime_mode, effective_mode_source
ORDER BY short_sessions DESC, sessions DESC, avg_duration_s ASC
LIMIT 20;
"

run_query "Short-Lived Drift Sessions" "
SELECT
  session_id,
  service_ref,
  state,
  COALESCE(NULLIF(trace_stop_reason, ''), reason) AS stop_reason,
  ROUND((updated_at_ms - created_at_ms) / 1000.0, 1) AS duration_s,
  policy_mode_hint,
  effective_runtime_mode,
  effective_mode_source,
  CASE WHEN client_path = '' THEN '(none)' ELSE client_path END AS client_path
FROM base
WHERE policy_mode_hint <> ''
  AND effective_runtime_mode <> ''
  AND policy_mode_hint <> effective_runtime_mode
  AND (updated_at_ms - created_at_ms) < $SHORT_THRESHOLD_MS
ORDER BY created_at_ms DESC
LIMIT 20;
"
