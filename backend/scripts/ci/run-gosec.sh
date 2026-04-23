#!/usr/bin/env bash
set -euo pipefail

if [[ $# -gt 1 ]]; then
    echo "usage: $0 [output-path]" >&2
    exit 2
fi

output_path="${1:-gosec.sarif}"

gosec_bin="$(command -v gosec || true)"
if [[ -z "${gosec_bin}" ]]; then
    gobin="$(go env GOPATH 2>/dev/null)/bin/gosec"
    if [[ -x "${gobin}" ]]; then
        gosec_bin="${gobin}"
    else
        echo "gosec not found in PATH or \$(go env GOPATH)/bin" >&2
        exit 127
    fi
fi

"${gosec_bin}" \
    -fmt=sarif \
    -out="${output_path}" \
    -severity=high \
    -tags=nogpu \
    -exclude-generated \
    -exclude-dir=internal/cache \
    -exclude-dir=test \
    -exclude-dir=internal/test \
    -exclude-dir=internal/api \
    -exclude-dir=.github \
    -exclude-dir=vendor \
    ./...
