#!/usr/bin/env bash
set -euo pipefail

# Guardrail: block NEW unimported internal packages (whole-package dead code).
#
# golangci-lint's unused check only sees symbols inside compiled packages, never
# a whole package that nothing imports (that is how internal/cache and internal/v3
# rotted unnoticed). A package is "dead" here when no other package imports it
# (build, test or external-test imports) and it is not a main package.
#
# The BASELINE enumerates packages currently unimported on purpose plus known
# orphans that are candidates for removal. Anything NEW that becomes unimported
# fails the gate. Ratchet the baseline DOWN as orphans are deleted.
SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
cd "${REPO_ROOT}/backend"
export GOWORK=off

BASELINE=(
  # Intentional: doc-only packages and the shared test helper.
  "github.com/ManuGH/xg2g/internal/control"
  "github.com/ManuGH/xg2g/internal/pipeline"
  "github.com/ManuGH/xg2g/internal/testutil"
  # Known orphans — candidates for removal (verify no codegen/reflection use first).
  "github.com/ManuGH/xg2g/internal/auth"
  "github.com/ManuGH/xg2g/internal/control/http/v3/types"
  "github.com/ManuGH/xg2g/internal/media/playback"
  "github.com/ManuGH/xg2g/internal/metrics/gpu"
  "github.com/ManuGH/xg2g/internal/types"
)

imported="$(go list -f '{{range .Imports}}{{.}}
{{end}}{{range .TestImports}}{{.}}
{{end}}{{range .XTestImports}}{{.}}
{{end}}' ./... 2>/dev/null | sort -u)"

violations=()
while IFS= read -r pkg; do
  [ -z "$pkg" ] && continue
  [ "$(go list -f '{{.Name}}' "$pkg" 2>/dev/null)" = "main" ] && continue
  grep -qxF "$pkg" <<<"$imported" && continue
  in_baseline=false
  for b in "${BASELINE[@]}"; do
    if [ "$b" = "$pkg" ]; then
      in_baseline=true
      break
    fi
  done
  $in_baseline || violations+=("$pkg")
done < <(go list ./internal/... 2>/dev/null)

if [ "${#violations[@]}" -gt 0 ]; then
  echo "❌ new unimported internal package(s) detected:"
  printf '  %s\n' "${violations[@]}"
  echo "Wire them up, delete them, or (if intentional) add to BASELINE in scripts/check-dead-packages.sh."
  exit 1
fi
echo "✅ no new unimported internal packages (baseline: ${#BASELINE[@]})"
