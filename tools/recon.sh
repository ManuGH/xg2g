#!/usr/bin/env bash
set -euo pipefail

# Reconnaissance script: quick scan + baseline capture for Beauty Pass
# Usage: from repo root: tools/recon.sh

# Ensure required tools are available or degrade gracefully
has_cmd() { command -v "$1" >/dev/null 2>&1; }

ROOT_DIR=$(pwd)
BASELINE_FILE="${ROOT_DIR}/BEAUTY_PASS_BASELINE.md"

info() { printf "[recon] %s\n" "$*"; }

# Section helpers
open_section() { echo "## $1" >>"${BASELINE_FILE}"; echo >>"${BASELINE_FILE}"; }
open_block() { echo '\n```' >>"${BASELINE_FILE}"; }
close_block() { echo '```' >>"${BASELINE_FILE}"; echo >>"${BASELINE_FILE}"; }

# Start fresh baseline
cat >"${BASELINE_FILE}" <<EOF
# Beauty Pass - Baseline before changes

Generated: $(date -u +"%Y-%m-%dT%H:%M:%SZ")
Repository: $(basename "${ROOT_DIR}")
Current branch: $(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "n/a")
Current commit: $(git rev-parse --short HEAD 2>/dev/null || echo "n/a")
EOF

echo >>"${BASELINE_FILE}"

# Build status
open_section "Build Status"
open_block
if has_cmd go; then
  (set +e; go build ./cmd/daemon 2>&1; echo "EXIT_CODE=$?" ) | tee -a "${BASELINE_FILE}" >/dev/null
else
  echo "go not installed" >>"${BASELINE_FILE}"
fi
close_block

# Test status
open_section "Test Status"
open_block
if has_cmd go; then
  (set +e; go test ./... -v 2>&1; echo "EXIT_CODE=$?" ) | tee -a "${BASELINE_FILE}" >/dev/null
else
  echo "go not installed" >>"${BASELINE_FILE}"
fi
close_block

# Linter status
open_section "Linter Status (golangci-lint)"
open_block
if has_cmd golangci-lint; then
  (set +e; golangci-lint run 2>&1; echo "EXIT_CODE=$?" ) | tee -a "${BASELINE_FILE}" >/dev/null
else
  echo "golangci-lint not installed" >>"${BASELINE_FILE}"
fi
close_block

# File counts
open_section "File Count"
{
  echo "- Go files: $(find . -type f -name '*.go' -not -path '*/vendor/*' | wc -l | xargs)"
  echo "- Test files: $(find . -type f -name '*_test.go' -not -path '*/vendor/*' | wc -l | xargs)"
  echo "- Markdown files: $(find . -type f -name '*.md' | wc -l | xargs)"
  echo "- YAML files: $(find . -type f \( -name '*.yml' -o -name '*.yaml' \) | wc -l | xargs)"
} >>"${BASELINE_FILE}"

echo >>"${BASELINE_FILE}"
open_section "Existing Files Check"
{
  [[ -f LICENSE ]] && echo "- [x] LICENSE: EXISTS" || echo "- [ ] LICENSE: MISSING"
  [[ -f SECURITY.md ]] && echo "- [x] SECURITY.md: EXISTS" || echo "- [ ] SECURITY.md: MISSING"
  [[ -f .editorconfig ]] && echo "- [x] .editorconfig: EXISTS" || echo "- [ ] .editorconfig: MISSING"
  [[ -f CONTRIBUTING.md ]] && echo "- [x] CONTRIBUTING.md: EXISTS" || echo "- [ ] CONTRIBUTING.md: MISSING"
  [[ -f CHANGELOG.md ]] && echo "- [x] CHANGELOG.md: EXISTS" || echo "- [ ] CHANGELOG.md: MISSING"
  [[ -f .env.example ]] && echo "- [x] .env.example: EXISTS" || echo "- [ ] .env.example: MISSING"
} >>"${BASELINE_FILE}"

echo >>"${BASELINE_FILE}"
open_section "Quick Scan Results"
open_block
{
  echo "gofmt (files needing format):"
  if has_cmd gofmt; then
    gofmt -l . | tee /dev/stderr | wc -l | xargs echo "count:" 1>&2
    # Note: file list printed to stderr; count emitted to stderr too for console visibility
  else
    echo "gofmt not installed"
  fi
  echo
  echo "goimports (files needing import fix):"
  if has_cmd goimports; then
    goimports -l . | tee /dev/stderr | wc -l | xargs echo "count:" 1>&2
  else
    echo "goimports not installed"
  fi
  echo
  echo "markdownlint issues:"
  if has_cmd markdownlint; then
    markdownlint "**/*.md" || true
  else
    echo "markdownlint not installed"
  fi
  echo
  echo "yamllint issues:"
  if has_cmd yamllint; then
    yamllint . || true
  else
    echo "yamllint not installed"
  fi
  echo
  echo "GitHub Actions floating tags (@main/@master):"
  if [[ -d .github/workflows ]]; then
    grep -R "uses:.*@main" .github/workflows || echo "No @main tags"
    grep -R "uses:.*@master" .github/workflows || echo "No @master tags"
  else
    echo "No workflows directory"
  fi
} >>"${BASELINE_FILE}"
close_block

info "Baseline written to ${BASELINE_FILE}"

# Optional console summary
echo
echo "Summary:"
echo "- Baseline: ${BASELINE_FILE}"
# Use printf with explicit format and prefix to avoid option parsing on leading dashes
go_files_count=$(find . -type f -name '*.go' -not -path '*/vendor/*' | wc -l | xargs)
test_files_count=$(find . -type f -name '*_test.go' -not -path '*/vendor/*' | wc -l | xargs)
printf '%s %s\n' "- Go files:" "${go_files_count}"
printf '%s %s\n' "- Test files:" "${test_files_count}"
