#!/usr/bin/env bash
set -euo pipefail

OWNER=${1:?usage: $0 <OWNER> [ALPINE_DIGEST] [DISTROLESS_DIGEST]}
ALPINE_DIGEST=${2:-}
DISTROLESS_DIGEST=${3:-}

update_file() {
  local file="$1"
  local owner="$2"
  local alpine="$3"
  local dist="$4"

  tmp=$(mktemp)
  cp "$file" "$tmp"

  sed -i.bak "s#ghcr.io/<OWNER>#ghcr.io/${owner}#g" "$tmp"
  if [[ -n "$alpine" ]]; then
    sed -i.bak "s#sha256:<ALPINE_DIGEST>#sha256:${alpine}#g" "$tmp"
  fi
  if [[ -n "$dist" ]]; then
    sed -i.bak "s#sha256:<DISTROLESS_DIGEST>#sha256:${dist}#g" "$tmp"
  fi

  mv "$tmp" "$file"
  rm -f "$tmp" "$tmp.bak" 2>/dev/null || true
}

update_file "deploy/docker-compose.alpine.yml" "$OWNER" "$ALPINE_DIGEST" "$DISTROLESS_DIGEST"
update_file "deploy/docker-compose.distroless.yml" "$OWNER" "$ALPINE_DIGEST" "$DISTROLESS_DIGEST"
update_file "deploy/k8s-alpine.yaml" "$OWNER" "$ALPINE_DIGEST" "$DISTROLESS_DIGEST"
update_file "deploy/k8s-distroless.yaml" "$OWNER" "$ALPINE_DIGEST" "$DISTROLESS_DIGEST"

echo "✅ Updated deploy templates with owner=${OWNER}, alpine=${ALPINE_DIGEST:-<unchanged>}, distroless=${DISTROLESS_DIGEST:-<unchanged>}"
