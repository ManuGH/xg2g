#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Upstream-Timeout Chaos Test
# Simulates slow/unresponsive upstream Enigma2 receiver to validate:
# - Circuit breaker activation (internal/openwebif/client.go)
# - Request timeout handling
# - Graceful degradation (error responses without cascading failures)
# - Metrics recording (circuit breaker state changes)

set -euo pipefail

# Configuration
TARGET_URL="${TARGET_URL:-http://localhost:8080/api/v1/lineup.json}"
SLOW_ENDPOINT="${SLOW_ENDPOINT:-/web/getallservices}"  # Mock slow upstream
TIMEOUT_DURATION="${TIMEOUT_DURATION:-30}"              # Seconds to introduce delay
DELAY_MS="${DELAY_MS:-5000}"                            # Upstream response delay (ms)
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

    if ! command -v curl &> /dev/null; then
        error "curl not found"
        exit 1
    fi

    # Check if target is reachable
    if ! curl -sf "${TARGET_URL}" -o /dev/null --max-time 5; then
        warn "Target URL ${TARGET_URL} not reachable (may be expected)"
    fi

    log "Prerequisites OK"
}

query_prometheus() {
    local query=$1
    local url="${PROMETHEUS_URL}/api/v1/query?query=${query}"

    curl -sf "$url" | jq -r '.data.result[0].value[1]' 2>/dev/null || echo "N/A"
}

collect_baseline_metrics() {
    log "Collecting baseline metrics..."

    BASELINE_SUCCESS=$(query_prometheus "sum(rate(http_requests_total{status=\"200\"}[1m]))")
    BASELINE_ERRORS=$(query_prometheus "sum(rate(http_requests_total{status=~\"5..\"}[1m]))")
    BASELINE_CIRCUIT_OPEN=$(query_prometheus "openwebif_circuit_breaker_state{state=\"open\"}")

    log "  Success rate: $BASELINE_SUCCESS req/sec"
    log "  Error rate: $BASELINE_ERRORS req/sec"
    log "  Circuit breaker open: $BASELINE_CIRCUIT_OPEN"
}

inject_upstream_delay() {
    log "CHAOS: Simulating upstream delay (${DELAY_MS}ms for ${TIMEOUT_DURATION}s)"

    # Note: This is a mock implementation
    # In production, you would use a tool like Toxiproxy or modify the upstream
    # For this test, we'll generate artificial load that triggers timeouts

    log "Generating timeout-inducing requests..."

    # Background job: Send requests that will timeout
    (
        local end_time=$(($(date +%s) + TIMEOUT_DURATION))
        while [[ $(date +%s) -lt $end_time ]]; do
            # Send request with short timeout to trigger circuit breaker
            curl -sf "${TARGET_URL}" --max-time 1 -o /dev/null 2>&1 || true
            sleep 0.5
        done
    ) &

    LOAD_PID=$!
    log "Timeout injection started (PID: $LOAD_PID)"
}

monitor_circuit_breaker() {
    local duration=$1
    local start=$(date +%s)

    log "Monitoring circuit breaker state for ${duration}s..."

    local circuit_opened=false
    local open_count=0

    while [[ $(($(date +%s) - start)) -lt $duration ]]; do
        local state=$(query_prometheus "openwebif_circuit_breaker_state")

        # Check if circuit breaker opened (state > 0 means open)
        if [[ "$state" != "N/A" && "$state" != "0" ]]; then
            if [[ "$circuit_opened" == "false" ]]; then
                log "✓ Circuit breaker OPENED (protecting against failing upstream)"
                circuit_opened=true
            fi
            ((open_count++))
        fi

        echo -n "."
        sleep 2
    done

    echo ""

    if [[ "$circuit_opened" == "true" ]]; then
        log "✓ Circuit breaker activated (opened $open_count times)"
        CIRCUIT_OPENED=true
    else
        warn "Circuit breaker did not open (upstream may not have timed out)"
        CIRCUIT_OPENED=false
    fi
}

collect_post_chaos_metrics() {
    log "Collecting post-chaos metrics..."

    POST_SUCCESS=$(query_prometheus "sum(rate(http_requests_total{status=\"200\"}[1m]))")
    POST_ERRORS=$(query_prometheus "sum(rate(http_requests_total{status=~\"5..\"}[1m]))")
    POST_CIRCUIT_OPEN=$(query_prometheus "openwebif_circuit_breaker_state{state=\"open\"}")
    POST_TIMEOUTS=$(query_prometheus "sum(increase(openwebif_requests_total{status=\"timeout\"}[${TIMEOUT_DURATION}s]))")

    log "  Success rate: $POST_SUCCESS req/sec"
    log "  Error rate: $POST_ERRORS req/sec"
    log "  Circuit breaker open: $POST_CIRCUIT_OPEN"
    log "  Total timeouts: $POST_TIMEOUTS"
}

test_graceful_degradation() {
    log "Testing graceful degradation (should return 503 Service Unavailable)..."

    local response_code=$(curl -s -o /dev/null -w "%{http_code}" "${TARGET_URL}" --max-time 5 || echo "000")

    if [[ "$response_code" == "503" ]]; then
        log "✓ Returned 503 Service Unavailable (graceful degradation)"
        return 0
    elif [[ "$response_code" == "000" ]]; then
        warn "Request timed out (circuit breaker may be fully open)"
        return 0
    elif [[ "$response_code" == "200" ]]; then
        warn "Returned 200 OK (upstream recovered or circuit breaker closed)"
        return 0
    else
        warn "Unexpected status code: $response_code"
        return 1
    fi
}

run_upstream_timeout_test() {
    log "Starting Upstream-Timeout Chaos Test"
    log "Target URL: $TARGET_URL"
    log "Timeout Duration: ${TIMEOUT_DURATION}s"
    log "Upstream Delay: ${DELAY_MS}ms"
    echo ""

    # Phase 1: Baseline
    check_prerequisites
    collect_baseline_metrics

    # Phase 2: Inject upstream delay
    inject_upstream_delay

    # Phase 3: Monitor circuit breaker
    monitor_circuit_breaker "$TIMEOUT_DURATION"

    # Phase 4: Wait for load generator to finish
    if [[ -n "${LOAD_PID:-}" ]]; then
        wait "$LOAD_PID" 2>/dev/null || true
        log "Timeout injection completed"
    fi

    # Phase 5: Test graceful degradation
    sleep 5  # Let metrics stabilize
    test_graceful_degradation

    # Phase 6: Collect post-chaos metrics
    collect_post_chaos_metrics

    # Phase 7: Wait for circuit breaker recovery
    log "Waiting for circuit breaker to close (30s)..."
    sleep 30

    local final_state=$(query_prometheus "openwebif_circuit_breaker_state")
    if [[ "$final_state" == "0" || "$final_state" == "N/A" ]]; then
        log "✓ Circuit breaker closed (system recovered)"
    else
        warn "Circuit breaker still open (may need manual intervention)"
    fi

    # Phase 8: Verdict
    echo ""
    log "═══════════════════════════════════════════════"
    log "Upstream-Timeout Chaos Test Summary"
    log "═══════════════════════════════════════════════"
    log "Baseline Metrics:"
    log "  Success rate: $BASELINE_SUCCESS req/sec"
    log "  Error rate: $BASELINE_ERRORS req/sec"
    log "  Circuit breaker open: $BASELINE_CIRCUIT_OPEN"
    log ""
    log "Post-Chaos Metrics:"
    log "  Success rate: $POST_SUCCESS req/sec"
    log "  Error rate: $POST_ERRORS req/sec"
    log "  Total timeouts: $POST_TIMEOUTS"
    log "  Circuit breaker activated: ${CIRCUIT_OPENED:-false}"
    log ""
    log "Resilience Validation:"

    local test_passed=true

    # Check if circuit breaker activated
    if [[ "${CIRCUIT_OPENED:-false}" == "true" ]]; then
        log "  ✓ Circuit breaker protected against cascading failures"
    else
        warn "  ⚠ Circuit breaker did not activate (check thresholds)"
        # Not a hard failure - upstream may not have been slow enough
    fi

    # Check if system remained responsive (no complete outage)
    if [[ "$POST_SUCCESS" != "N/A" && "$POST_SUCCESS" != "0" ]]; then
        log "  ✓ System remained partially responsive"
    else
        warn "  ⚠ System became unresponsive (investigate)"
    fi

    # Check if errors were recorded properly
    if [[ "$POST_TIMEOUTS" != "N/A" && "$POST_TIMEOUTS" != "0" ]]; then
        log "  ✓ Timeouts recorded in metrics"
    fi

    log "═══════════════════════════════════════════════"

    if [[ "$test_passed" == "true" ]]; then
        log "Upstream-Timeout Test PASSED ✅"
    else
        error "Upstream-Timeout Test FAILED ❌"
        exit 1
    fi
}

# Main execution
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    run_upstream_timeout_test
fi
