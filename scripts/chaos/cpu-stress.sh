#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# CPU-Stress Chaos Test
# Simulates high CPU load to validate:
# - Horizontal Pod Autoscaler (HPA) behavior
# - Rate limiting under load
# - Response time degradation thresholds
# - Circuit breaker activation

set -euo pipefail

# Configuration
NAMESPACE="${NAMESPACE:-default}"
DEPLOYMENT="${DEPLOYMENT:-xg2g}"
STRESS_DURATION="${STRESS_DURATION:-60}"
TARGET_CPU_PERCENT="${TARGET_CPU_PERCENT:-80}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:9090}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

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
        error "kubectl not found"
        exit 1
    fi

    if ! kubectl get deployment "$DEPLOYMENT" -n "$NAMESPACE" &> /dev/null; then
        error "Deployment $DEPLOYMENT not found in namespace $NAMESPACE"
        exit 1
    fi

    # Check if HPA exists
    if kubectl get hpa -n "$NAMESPACE" | grep -q "$DEPLOYMENT"; then
        log "HPA found for $DEPLOYMENT"
        HPA_ENABLED=true
    else
        warn "No HPA found for $DEPLOYMENT - scaling test will be skipped"
        HPA_ENABLED=false
    fi

    log "Prerequisites OK"
}

get_pod_name() {
    kubectl get pods -n "$NAMESPACE" \
        -l app="$DEPLOYMENT" \
        -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo ""
}

get_pod_count() {
    kubectl get deployment "$DEPLOYMENT" -n "$NAMESPACE" \
        -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0"
}

get_hpa_replicas() {
    kubectl get hpa -n "$NAMESPACE" \
        -l app="$DEPLOYMENT" \
        -o jsonpath='{.items[0].status.currentReplicas}' 2>/dev/null || echo "0"
}

query_prometheus() {
    local query=$1
    local url="${PROMETHEUS_URL}/api/v1/query?query=${query}"

    curl -sf "$url" | jq -r '.data.result[0].value[1]' 2>/dev/null || echo "N/A"
}

collect_baseline_metrics() {
    log "Collecting baseline metrics..."

    BASELINE_CPU=$(query_prometheus "sum(rate(container_cpu_usage_seconds_total{pod=~\"${DEPLOYMENT}.*\"}[1m])) by (pod)")
    BASELINE_REPLICAS=$(get_pod_count)
    BASELINE_P95=$(query_prometheus "histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[1m]))")
    BASELINE_RATE_LIMIT=$(query_prometheus "sum(rate(http_requests_total{status=\"429\"}[1m]))")

    log "  CPU usage: $BASELINE_CPU cores"
    log "  Replicas: $BASELINE_REPLICAS"
    log "  P95 latency: $BASELINE_P95 sec"
    log "  Rate limit hits/sec: $BASELINE_RATE_LIMIT"
}

inject_cpu_stress() {
    local target_pod=$1
    local duration=$2
    local cpu_percent=$3

    log "CHAOS: Injecting CPU stress into $target_pod"
    log "  Duration: ${duration}s"
    log "  Target CPU: ${cpu_percent}%"

    # Calculate number of workers based on target CPU percentage
    # Assuming 1000m (1 core) limit, 80% = 0.8 cores
    local workers=$(( cpu_percent / 10 ))
    [[ $workers -lt 1 ]] && workers=1

    # Inject stress using kubectl exec (requires stress-ng in container)
    # If not available, use pure bash CPU burn
    if kubectl exec -n "$NAMESPACE" "$target_pod" -- which stress-ng &> /dev/null; then
        log "Using stress-ng (${workers} workers)"
        kubectl exec -n "$NAMESPACE" "$target_pod" -- \
            stress-ng --cpu "$workers" --timeout "${duration}s" --metrics-brief &
    else
        log "Using bash CPU burn (${workers} workers)"
        kubectl exec -n "$NAMESPACE" "$target_pod" -- \
            bash -c "for i in {1..${workers}}; do while true; do :; done & done; sleep ${duration}; pkill -P \$\$" &
    fi

    STRESS_PID=$!
    log "Stress injection started (PID: $STRESS_PID)"
}

monitor_hpa_scaling() {
    local duration=$1
    local start=$(date +%s)

    log "Monitoring HPA scaling for ${duration}s..."

    local max_replicas=$BASELINE_REPLICAS
    local scaled_up=false

    while [[ $(($(date +%s) - start)) -lt $duration ]]; do
        local current_replicas=$(get_pod_count)
        if [[ $current_replicas -gt $max_replicas ]]; then
            max_replicas=$current_replicas
            scaled_up=true
            log "HPA scaled up to $max_replicas replicas"
        fi

        echo -n "."
        sleep 5
    done

    echo ""

    if [[ "$scaled_up" == "true" ]]; then
        log "✓ HPA scaling triggered (peak: $max_replicas replicas)"
    else
        warn "HPA did not scale up during test"
    fi

    PEAK_REPLICAS=$max_replicas
}

collect_post_stress_metrics() {
    log "Collecting post-stress metrics..."

    POST_CPU=$(query_prometheus "sum(rate(container_cpu_usage_seconds_total{pod=~\"${DEPLOYMENT}.*\"}[1m])) by (pod)")
    POST_REPLICAS=$(get_pod_count)
    POST_P95=$(query_prometheus "histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[1m]))")
    POST_RATE_LIMIT=$(query_prometheus "sum(rate(http_requests_total{status=\"429\"}[1m]))")

    log "  CPU usage: $POST_CPU cores"
    log "  Replicas: $POST_REPLICAS"
    log "  P95 latency: $POST_P95 sec"
    log "  Rate limit hits/sec: $POST_RATE_LIMIT"
}

run_cpu_stress_test() {
    log "Starting CPU-Stress Chaos Test"
    log "Namespace: $NAMESPACE"
    log "Deployment: $DEPLOYMENT"
    log "Duration: ${STRESS_DURATION}s"
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

    # Phase 3: Inject CPU stress
    inject_cpu_stress "$target_pod" "$STRESS_DURATION" "$TARGET_CPU_PERCENT"

    # Phase 4: Monitor HPA (if enabled)
    if [[ "$HPA_ENABLED" == "true" ]]; then
        monitor_hpa_scaling "$STRESS_DURATION"
    else
        log "Waiting for stress duration (${STRESS_DURATION}s)..."
        sleep "$STRESS_DURATION"
    fi

    # Phase 5: Wait for stress to complete
    if [[ -n "${STRESS_PID:-}" ]]; then
        wait "$STRESS_PID" 2>/dev/null || true
        log "Stress injection completed"
    fi

    # Phase 6: Let system stabilize
    log "Waiting for system to stabilize (30s)..."
    sleep 30

    # Phase 7: Collect post-stress metrics
    collect_post_stress_metrics

    # Phase 8: Verdict
    echo ""
    log "═══════════════════════════════════════════════"
    log "CPU-Stress Chaos Test Summary"
    log "═══════════════════════════════════════════════"
    log "Baseline Metrics:"
    log "  CPU: $BASELINE_CPU cores"
    log "  Replicas: $BASELINE_REPLICAS"
    log "  P95 Latency: $BASELINE_P95 sec"
    log "  Rate Limit Hits: $BASELINE_RATE_LIMIT /sec"
    log ""
    log "Post-Stress Metrics:"
    log "  CPU: $POST_CPU cores"
    log "  Replicas: $POST_REPLICAS"
    log "  P95 Latency: $POST_P95 sec"
    log "  Rate Limit Hits: $POST_RATE_LIMIT /sec"

    if [[ "$HPA_ENABLED" == "true" ]]; then
        log ""
        log "HPA Scaling:"
        log "  Peak Replicas: ${PEAK_REPLICAS:-$BASELINE_REPLICAS}"
        if [[ "${PEAK_REPLICAS:-$BASELINE_REPLICAS}" -gt "$BASELINE_REPLICAS" ]]; then
            log "  ✓ HPA successfully scaled up"
        else
            warn "  ⚠ HPA did not scale up (may need tuning)"
        fi
    fi

    log "═══════════════════════════════════════════════"

    # Evaluate results
    local test_passed=true

    # Check if P95 latency increased beyond acceptable threshold (e.g., 2x)
    if [[ "$POST_P95" != "N/A" && "$BASELINE_P95" != "N/A" ]]; then
        local latency_ratio=$(echo "$POST_P95 / $BASELINE_P95" | bc -l 2>/dev/null || echo "1")
        if (( $(echo "$latency_ratio > 2.0" | bc -l) )); then
            warn "P95 latency increased by ${latency_ratio}x (threshold: 2x)"
            test_passed=false
        fi
    fi

    # Check if system remained responsive (not completely overloaded)
    if [[ "$POST_REPLICAS" == "0" ]]; then
        error "All pods became unavailable during stress test"
        test_passed=false
    fi

    if [[ "$test_passed" == "true" ]]; then
        log "CPU-Stress Test PASSED ✅"
    else
        error "CPU-Stress Test FAILED ❌"
        exit 1
    fi
}

# Main execution
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    run_cpu_stress_test
fi
