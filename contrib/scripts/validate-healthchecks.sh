#!/usr/bin/env bash
set -euo pipefail

# Healthcheck Endpoint Validation Hook
#
# This pre-commit hook ensures:
# 1. Docker Compose files use /healthz (liveness), NOT /readyz (readiness)
# 2. Kubernetes manifests use both:
#    - livenessProbe: /healthz
#    - readinessProbe: /readyz
#
# Rationale:
# - Docker Compose healthchecks are for liveness checking (is container alive?)
# - Kubernetes has separate probes for liveness and readiness
# - Using /readyz in Docker Compose causes false "unhealthy" status during startup

error=0

echo "[validate-healthchecks] Checking health check endpoint usage..."

# Get list of modified files from git
files_to_check=()
while IFS= read -r file; do
  if [[ -f "$file" ]]; then
    case "$file" in
      docker-compose*.yml|docker-compose*.yaml)
        files_to_check+=("$file")
        ;;
      deploy/docker-compose*.yml|deploy/docker-compose*.yaml)
        files_to_check+=("$file")
        ;;
    esac
  fi
done < <(git diff --cached --name-only --diff-filter=ACM)

if [ "${#files_to_check[@]}" -eq 0 ]; then
  echo "[validate-healthchecks] No docker-compose files modified. Skipping."
  exit 0
fi

echo "[validate-healthchecks] Checking ${#files_to_check[@]} file(s)..."

for f in "${files_to_check[@]}"; do
  echo "[validate-healthchecks] Validating: $f"

  # ERROR: /readyz in docker-compose files
  if grep -q "path.*:/readyz\|test.*readyz" "$f"; then
    echo "❌ ERROR: /readyz found in $f"
    echo "   Docker Compose healthchecks must use /healthz (liveness probe)"
    echo "   /readyz returns 503 until service is ready, causing false 'unhealthy' status"
    echo ""
    echo "   Fix: Change healthcheck endpoint to /healthz"
    echo "   Example:"
    echo "     healthcheck:"
    echo "       test: wget -q -T 5 -O /dev/null http://localhost:8080/healthz || exit 1"
    echo ""
    error=1
  fi

  # WARNING: healthcheck without /healthz
  if grep -qi "healthcheck" "$f"; then
    if ! grep -q "/healthz" "$f"; then
      echo "⚠️  WARNING: healthcheck found in $f but no /healthz endpoint"
      echo "   Expected endpoint: /healthz (liveness probe)"
      echo ""
    fi
  fi

  # INFO: Check for HEAD requests (common mistake)
  if grep -q "wget.*--spider.*healthz\|wget.*--spider.*readyz" "$f"; then
    echo "⚠️  WARNING: wget --spider detected in $f"
    echo "   --spider uses HEAD requests, but health endpoints expect GET"
    echo "   Remove --spider flag to use GET requests"
    echo ""
  fi
done

# Check Kubernetes manifests if they exist
k8s_files=()
while IFS= read -r file; do
  if [[ -f "$file" ]]; then
    case "$file" in
      deploy/k8s-*.yaml|deploy/kubernetes/*.yaml)
        k8s_files+=("$file")
        ;;
    esac
  fi
done < <(git diff --cached --name-only --diff-filter=ACM)

if [ "${#k8s_files[@]}" -gt 0 ]; then
  echo "[validate-healthchecks] Checking ${#k8s_files[@]} Kubernetes manifest(s)..."

  for f in "${k8s_files[@]}"; do
    echo "[validate-healthchecks] Validating: $f"

    # Check if file has probes
    if ! grep -q "Probe:" "$f"; then
      continue
    fi

    # Verify livenessProbe uses /healthz
    if grep -q "livenessProbe:" "$f"; then
      if ! grep -A5 "livenessProbe:" "$f" | grep -q "path: /healthz"; then
        echo "❌ ERROR: livenessProbe in $f does not use /healthz"
        echo "   livenessProbe should use /healthz (process alive check)"
        error=1
      fi
    fi

    # Verify readinessProbe uses /readyz
    if grep -q "readinessProbe:" "$f"; then
      if ! grep -A5 "readinessProbe:" "$f" | grep -q "path: /readyz"; then
        echo "❌ ERROR: readinessProbe in $f does not use /readyz"
        echo "   readinessProbe should use /readyz (service ready check)"
        error=1
      fi
    fi
  done
fi

if [ "$error" -ne 0 ]; then
  echo ""
  echo "[validate-healthchecks] ❌ Validation failed"
  echo ""
  echo "Health check endpoint guidelines:"
  echo "  • Docker Compose: Use /healthz (liveness)"
  echo "  • Kubernetes livenessProbe: Use /healthz"
  echo "  • Kubernetes readinessProbe: Use /readyz"
  echo ""
  echo "See docs/operations/HEALTH_CHECKS.md for details"
  exit 1
fi

echo "[validate-healthchecks] ✅ All health check endpoints are correct"
exit 0
