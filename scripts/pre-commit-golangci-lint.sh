#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

gobin="$(go env GOBIN)"
if [[ -n "$gobin" ]]; then
  tool="$gobin/golangci-lint"
else
  tool="$(go env GOPATH)/bin/golangci-lint"
fi

if [[ ! -x "$tool" ]]; then
  echo "ERROR: golangci-lint not found at $tool"
  echo "Run: make dev-tools"
  exit 1
fi

expected="$(sed -nE 's/^GOLANGCI_LINT_VERSION[[:space:]]*:?=[[:space:]]*(v[0-9]+\.[0-9]+\.[0-9]+).*/\1/p' Makefile | head -1)"
if [[ -z "$expected" ]]; then
  echo "ERROR: could not read GOLANGCI_LINT_VERSION from Makefile"
  exit 1
fi

actual="$("$tool" version 2>/dev/null | sed -nE 's/.*version ([0-9]+\.[0-9]+\.[0-9]+).*/v\1/p' | head -1)"
if [[ "$actual" != "$expected" ]]; then
  echo "ERROR: golangci-lint version mismatch: expected $expected, got ${actual:-unknown}"
  echo "Run: make dev-tools"
  exit 1
fi

exec "$tool" run --timeout=3m ./...
