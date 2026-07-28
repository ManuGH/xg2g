#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
WEBUI_ROOT="${REPO_ROOT}/apps/webui"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

[[ -d "${WEBUI_ROOT}/src" ]] || fail "missing WebUI source: ${WEBUI_ROOT}/src"

# React Router 7.18.1 is retained for its client-side security fixes while the
# upstream RSC action CSRF advisory has no published fixed release. xg2g does
# not use React Server Components or data-router actions. Keep that boundary
# executable so a future import cannot silently make the advisory applicable.
if git -C "${REPO_ROOT}" grep -n -E \
  '(react-router/(dom/)?server|RSCHydratedRouter|RSCStaticRouter|createCallServer|createServerReference|registerServerReference|decodeAction|decodeReply|createBrowserRouter|createHashRouter|RouterProvider)' \
  -- 'apps/webui/src/**'; then
  fail "WebUI must remain client-router-only until the React Router RSC advisory has a fixed release"
fi

echo "OK: WebUI router remains client-only (no RSC or data-router action surface)."
