#!/usr/bin/env bash
set -euo pipefail

go test ./internal/control/http/v3 -run TestRouterParity_
