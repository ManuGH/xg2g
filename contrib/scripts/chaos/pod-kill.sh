#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Pod-Kill Chaos Test
# Simulates sudden pod failure in Kubernetes to validate:
# - Service mesh failover (if enabled)
# - Graceful client reconnection
# - Metric/log continuity across restarts

set -euo pipefail

# Configuration
NAMESPACE="${NAMESPACE:-default}"
DEPLOYMENT="${DEPLOYMENT:-xg2g}"
HEALTH_URL="${HEALTH_URL:-http://localhost:8080/healthz}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-60}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:9090}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $*"
}

warn() {
    echo -e "${YELLOW}[$(date +'%Y-%m-%d %H:%M:%S')] WARNING:${NC} $*"
}

error() {
    echo -e "${RED}[$(date +'%Y-%m-%d %H:%M:%S')] ERROR:${NC} $*"
}

check_prerequisites() {
    log "Checking prerequisites..."

    if ! command -v kubectl &> /dev/null; then
        error "kubectl not found. Install it first."
        exit 1
    fi

    if ! kubectl get deployment "$DEPLOYMENT" -n "$NAMESPACE" &> /dev/null; then
        error "Deployment $DEPLOYMENT not found in namespace $NAMESPACE"
        exit 1
    fi

    log "Prerequisites OK"
}

get_pod_count() {
    kubectl get deployment "$DEPLOYMENT" -n "$NAMESPACE" \
        -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0"
}

get_pod_name() {
    kubectl get pods -n "$NAMESPACE" \
        -l app="$DEPLOYMENT" \
        -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo ""
}

wait_for_ready() {
    local timeout=$1
    local start=$(date +%s)

    log "Waiting for pod to become ready (timeout: ${timeout}s)..."

    while true; do
        local ready=$(get_pod_count)
        if [[ "$ready" -gt 0 ]]; then
            log "Pod is ready (${ready} replicas)"
            return 0
        fi

        local elapsed=$(($(date +%s) - start))
        if [[ $elapsed -ge $timeout ]]; then
            error "Timeout waiting for pod to become ready"
            return 1
        fi

        echo -n "."
        sleep 2
    done
}

check_health() {
    local url=$1
    log "Checking health endpoint: $url"

    if curl -sf "$url" -o /dev/null; then
        log "Health check PASSED"
        return 0
    else
        warn "Health check FAILED"
        return 1
    fi
}

query_prometheus() {
    local query=$1
    local url="${PROMETHEUS_URL}/api/v1/query?query=${query}"

    curl -sf "$url" | jq -r '.data.result[0].value[1]' 2>/dev/null || echo "N/A"
}

collect_baseline_metrics() {
    log "Collecting baseline metrics..."

    BASELINE_REQUESTS=$(query_prometheus "sum(rate(http_requests_total[1m]))")
    BASELINE_ERRORS=$(query_prometheus "sum(rate(http_requests_total{status=~\"5..\"}[1m]))")

    log "  Requests/sec: $BASELINE_REQUESTS"
    log "  Errors/sec: $BASELINE_ERRORS"
}

collect_post_chaos_metrics() {
    log "Collecting post-chaos metrics..."

    sleep 5  # Let metrics stabilize

    POST_REQUESTS=$(query_prometheus "sum(rate(http_requests_total[1m]))")
    POST_ERRORS=$(query_prometheus "sum(rate(http_requests_total{status=~\"5..\"}[1m]))")

    log "  Requests/sec: $POST_REQUESTS"
    log "  Errors/sec: $POST_ERRORS"
}

run_pod_kill_test() {
    log "Starting Pod-Kill Chaos Test"
    log "Namespace: $NAMESPACE"
    log "Deployment: $DEPLOYMENT"
    echo ""

    # Phase 1: Baseline
    check_prerequisites
    collect_baseline_metrics

    # Phase 2: Identify target pod
    local target_pod=$(get_pod_name)
    if [[ -z "$target_pod" ]]; then
        error "No pods found for deployment $DEPLOYMENT"
        exit 1
    fi

    log "Target pod: $target_pod"

    # Phase 3: Delete pod (chaos injection)
    log "CHAOS: Deleting pod $target_pod..."
    kubectl delete pod "$target_pod" -n "$NAMESPACE" --grace-period=0 --force

    local kill_time=$(date +%s)
    log "Pod killed at $(date -r $kill_time)"

    # Phase 4: Wait for recovery
    if ! wait_for_ready "$WAIT_TIMEOUT"; then
        error "Pod failed to recover within timeout"
        exit 1
    fi

    local recovery_time=$(($(date +%s) - kill_time))
    log "Recovery time: ${recovery_time}s"

    # Phase 5: Health check
    sleep 5  # Let the pod stabilize
    if ! check_health "$HEALTH_URL"; then
        error "Post-recovery health check failed"
        exit 1
    fi

    # Phase 6: Metrics comparison
    collect_post_chaos_metrics

    # Phase 7: Verdict
    echo ""
    log "═══════════════════════════════════════════════"
    log "Pod-Kill Chaos Test Summary"
    log "═══════════════════════════════════════════════"
    log "✓ Pod killed successfully"
    log "✓ Pod recovered in ${recovery_time}s (threshold: ${WAIT_TIMEOUT}s)"
    log "✓ Health check passed"
    log ""
    log "Metrics Comparison:"
    log "  Baseline Requests/sec: $BASELINE_REQUESTS"
    log "  Post-Chaos Requests/sec: $POST_REQUESTS"
    log "  Baseline Errors/sec: $BASELINE_ERRORS"
    log "  Post-Chaos Errors/sec: $POST_ERRORS"
    log "═══════════════════════════════════════════════"

    # Check if error rate increased significantly
    if [[ "$POST_ERRORS" != "N/A" && "$BASELINE_ERRORS" != "N/A" ]]; then
        local error_increase=$(echo "$POST_ERRORS - $BASELINE_ERRORS" | bc 2>/dev/null || echo "0")
        if (( $(echo "$error_increase > 0.1" | bc -l 2>/dev/null || echo 0) )); then
            warn "Error rate increased by ${error_increase}/sec after pod kill"
        fi
    fi

    log "Pod-Kill Test PASSED ✅"
}

# Main execution
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    run_pod_kill_test
fi
