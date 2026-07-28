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

# React Router 8.3.0 contains the upstream RSC action CSRF fix. xg2g still uses
# only the declarative client router, so keep that architectural boundary
# executable and reject the removed react-router-dom compatibility package.
router_version="$(
  node -e '
    const pkg = require(process.argv[1]);
    process.stdout.write(pkg.dependencies?.["react-router"] ?? "");
  ' "${WEBUI_ROOT}/package.json"
)"
[[ "${router_version}" == "8.3.0" ]] || \
  fail "apps/webui must pin react-router exactly to 8.3.0 (found: ${router_version:-missing})"

if git -C "${REPO_ROOT}" grep -n -E \
  "(from|import\\()[[:space:]]*['\"]react-router-dom(['\"]|/)" \
  -- 'apps/webui/src/**' 'apps/webui/tests/**'; then
  fail "react-router-dom was removed in React Router 8; import from react-router"
fi

if git -C "${REPO_ROOT}" grep -n -E \
  '(react-router/(dom/)?server|RSCHydratedRouter|RSCStaticRouter|createCallServer|createServerReference|registerServerReference|decodeAction|decodeReply|createBrowserRouter|createHashRouter|RouterProvider)' \
  -- 'apps/webui/src/**'; then
  fail "WebUI must remain on the reviewed declarative client-router surface"
fi

echo "OK: WebUI pins React Router 8.3.0 and remains on the declarative client-router surface."
