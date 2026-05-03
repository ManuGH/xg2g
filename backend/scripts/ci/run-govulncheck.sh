#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
    echo "usage: $0 <format> <output-path>" >&2
    exit 2
fi

format="$1"
output_path="$2"

govulncheck_bin="$(command -v govulncheck || true)"
if [[ -z "${govulncheck_bin}" ]]; then
    gobin="$(go env GOPATH 2>/dev/null)/bin/govulncheck"
    if [[ -x "${gobin}" ]]; then
        govulncheck_bin="${gobin}"
    else
        echo "govulncheck not found in PATH or \$(go env GOPATH)/bin" >&2
        exit 127
    fi
fi

"${govulncheck_bin}" -tags=nogpu -format "${format}" ./... > "${output_path}"
