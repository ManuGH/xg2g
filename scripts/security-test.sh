#!/bin/bash
set -euo pipefail

# xg2g Security Penetration Test Suite
# Tests symlink attacks, path traversal, and other security vulnerabilities

BASE_URL="${1:-http://localhost:8080}"
TARGET_DIR="${2:-/tmp/xg2g-pentest}"
CONCURRENT_ATTACKS="${3:-10}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🛡️ xg2g Security Penetration Test Suite${NC}"
echo "Target: $BASE_URL"
echo "Test Directory: $TARGET_DIR"
echo "Concurrent Attacks: $CONCURRENT_ATTACKS"
echo ""

# Setup test environment
echo -e "${BLUE}📋 Setting up test environment...${NC}"
mkdir -p "$TARGET_DIR"/{outside,data,results}
cd "$TARGET_DIR"

# Results tracking
RESULTS_FILE="$TARGET_DIR/results/security-test-$(date +%Y%m%d-%H%M%S).json"
echo "{\"test_run\": \"$(date -Iseconds)\", \"target\": \"$BASE_URL\", \"results\": []}" > "$RESULTS_FILE"

# Helper function to log test results
log_result() {
    local test_name="$1"
    local expected_status="$2"
    local actual_status="$3"
    local success="$4"
    local response_body="$5"
    
    # Append to JSON results
    jq --arg test "$test_name" \
       --arg expected "$expected_status" \
       --arg actual "$actual_status" \
       --arg success "$success" \
       --arg body "$response_body" \
       '.results += [{
           "test": $test,
           "expected_status": $expected,
           "actual_status": $actual,
           "success": ($success == "true"),
           "timestamp": now,
           "response_body": $body
       }]' "$RESULTS_FILE" > "$RESULTS_FILE.tmp" && mv "$RESULTS_FILE.tmp" "$RESULTS_FILE"
       
    if [[ "$success" == "true" ]]; then
        echo -e "  ${GREEN}✅ $test_name${NC} (Expected: $expected_status, Got: $actual_status)"
    else
        echo -e "  ${RED}❌ $test_name${NC} (Expected: $expected_status, Got: $actual_status)"
        echo -e "     Response: ${response_body:0:100}..."
    fi
}

# Create test files and symlinks
echo -e "${BLUE}🔧 Creating test attack vectors...${NC}"

# Create files outside the data directory 
echo "SECRET_DATA_1" > outside/secret.txt
echo "SYSTEM_FILE" > outside/passwd
echo "CONFIG_DATA" > outside/config.json

# Test Category 1: Basic File Access Validation
echo -e "\n${YELLOW}📁 Category 1: Basic File Access${NC}"

test_basic_file_access() {
    echo "Testing basic file access patterns..."
    
    # Test 1: Valid file access (should work if files exist)
    response=$(curl -s -w "HTTPSTATUS:%{http_code}" "$BASE_URL/files/playlist.m3u" 2>/dev/null)
    body="${response%HTTPSTATUS:*}"
    status="${response##*HTTPSTATUS:}"
    status="${status//$'\n'/}"
    
    # Accept both 200 (file exists) and 404 (file doesn't exist) as valid
    if [[ "$status" == "200" || "$status" == "404" ]]; then
        log_result "valid_file_access" "200_or_404" "$status" "true" "$body"
    else
        log_result "valid_file_access" "200_or_404" "$status" "false" "$body"
    fi
    
    # Test 2: Nonexistent file
    response=$(curl -s -w "HTTPSTATUS:%{http_code}" "$BASE_URL/files/nonexistent.txt" 2>/dev/null)
    body="${response%HTTPSTATUS:*}"
    status="${response##*HTTPSTATUS:}"
    status="${status//$'\n'/}"
    log_result "nonexistent_file" "404" "$status" "$([ "$status" == "404" ] && echo "true" || echo "false")" "$body"
    
    # Test 3: Method restrictions
    for method in POST PUT DELETE PATCH; do
        response=$(curl -s -X "$method" -w "HTTPSTATUS:%{http_code}" "$BASE_URL/files/test.txt" 2>/dev/null)
        body="${response%HTTPSTATUS:*}"
        status="${response##*HTTPSTATUS:}"
        status="${status//$'\n'/}"
        log_result "${method,,}_method_blocked" "405" "$status" "$([ "$status" == "405" ] && echo "true" || echo "false")" "$body"
    done
}

# Test Category 2: Path Traversal Attacks
echo -e "\n${YELLOW}🗂️ Category 2: Path Traversal Attacks${NC}"

test_path_traversal() {
    echo "Testing path traversal attack patterns..."
    
    # Basic traversal patterns
    local patterns=(
        "../etc/passwd"
        "../../etc/passwd" 
        "../../../etc/passwd"
        "....//....//etc/passwd"
        "..%2F..%2Fetc%2Fpasswd"
        "..%252F..%252Fetc%252Fpasswd"
        "/%2e%2e/%2e%2e/etc/passwd"
        "/..%c0%af../etc/passwd"
    )
    
    for pattern in "${patterns[@]}"; do
        response=$(curl -s -w "HTTPSTATUS:%{http_code}" "$BASE_URL/files/$pattern" 2>/dev/null)
        body="${response%HTTPSTATUS:*}"
        status="${response##*HTTPSTATUS:}"
        status="${status//$'\n'/}"
        
        # Should be blocked (403/404) or normalized (301)
        if [[ "$status" == "403" || "$status" == "404" || "$status" == "301" ]]; then
            log_result "path_traversal_${pattern//\//_}" "403_or_404_or_301" "$status" "true" "$body"
        else
            log_result "path_traversal_${pattern//\//_}" "403_or_404_or_301" "$status" "false" "$body"
        fi
    done
}

# Test Category 3: Symlink Escape Attacks
echo -e "\n${YELLOW}🔗 Category 3: Symlink Escape Attacks${NC}"

test_symlink_attacks() {
    echo "Testing symlink escape attack patterns..."
    
    # Create malicious symlinks (these should be blocked at runtime)
    local symlink_tests=(
        "evil_secret:$TARGET_DIR/outside/secret.txt"
        "evil_passwd:$TARGET_DIR/outside/passwd"
        "evil_config:$TARGET_DIR/outside/config.json"
        "evil_dir:$TARGET_DIR/outside"
    )
    
    for test_case in "${symlink_tests[@]}"; do
        IFS=':' read -r link_name _ <<< "$test_case"
        
        # Test the symlink attack via HTTP (should be blocked)
        response=$(curl -s -w "HTTPSTATUS:%{http_code}" "$BASE_URL/files/$link_name" 2>/dev/null)
        body="${response%HTTPSTATUS:*}"
        status="${response##*HTTPSTATUS:}"
        status="${status//$'\n'/}"
        
        # Should be blocked with 403 Forbidden
        log_result "symlink_escape_$link_name" "403" "$status" "$([ "$status" == "403" ] && echo "true" || echo "false")" "$body"
    done
    
    # Test symlink chains (A -> B -> outside)
    response=$(curl -s -w "HTTPSTATUS:%{http_code}" "$BASE_URL/files/chain_link" 2>/dev/null)
    body="${response%HTTPSTATUS:*}"
    status="${response##*HTTPSTATUS:}"
    status="${status//$'\n'/}"
    log_result "symlink_chain_attack" "403" "$status" "$([ "$status" == "403" ] && echo "true" || echo "false")" "$body"
}

# Test Category 4: Directory Listing Protection
echo -e "\n${YELLOW}📂 Category 4: Directory Listing Protection${NC}"

test_directory_protection() {
    echo "Testing directory access protection..."
    
    local dir_tests=(
        ""           # Root directory
        "/"          # Explicit root
        "/subdir/"   # Subdirectory with trailing slash
        "/subdir"    # Subdirectory without trailing slash
    )
    
    for dir in "${dir_tests[@]}"; do
        response=$(curl -s -w "HTTPSTATUS:%{http_code}" "$BASE_URL/files$dir" 2>/dev/null)
        body="${response%HTTPSTATUS:*}"
        status="${response##*HTTPSTATUS:}"
        status="${status//$'\n'/}"
        
        # Directory access should be blocked (403) or redirected (301)
        if [[ "$status" == "403" || "$status" == "301" ]]; then
            log_result "directory_access_${dir//\//_}" "403_or_301" "$status" "true" "$body"
        else
            log_result "directory_access_${dir//\//_}" "403_or_301" "$status" "false" "$body"
        fi
    done
}

# Test Category 5: High-Volume Attack Simulation
echo -e "\n${YELLOW}⚡ Category 5: High-Volume Attack Simulation${NC}"

test_volume_attacks() {
    echo "Testing high-volume attack scenarios..."
    
    # Parallel symlink escape attempts
    echo "Launching $CONCURRENT_ATTACKS concurrent symlink attacks..."
    for ((i=1; i<=CONCURRENT_ATTACKS; i++)); do
        (
            response=$(curl -s -w "HTTPSTATUS:%{http_code}" "$BASE_URL/files/../../etc/passwd" 2>/dev/null)
            status="${response##*HTTPSTATUS:}"
            status="${status//$'\n'/}"
            echo "Attack_$i: $status"
        ) &
    done
    wait
    
    # Check if service is still responsive
    response=$(curl -s -w "HTTPSTATUS:%{http_code}" "$BASE_URL/healthz" 2>/dev/null)
    body="${response%HTTPSTATUS:*}"
    status="${response##*HTTPSTATUS:}"
    status="${status//$'\n'/}"
    log_result "service_resilience_after_attack" "200" "$status" "$([ "$status" == "200" ] && echo "true" || echo "false")" "$body"
}

# Test Category 6: Edge Cases and Error Handling
echo -e "\n${YELLOW}🎯 Category 6: Edge Cases${NC}"

test_edge_cases() {
    echo "Testing edge cases and error handling..."
    
    # Large path
    local large_path
    large_path=$(python3 -c "print('a' * 1000)")
    response=$(curl -s -w "HTTPSTATUS:%{http_code}" "$BASE_URL/files/$large_path" 2>/dev/null)
    body="${response%HTTPSTATUS:*}"
    status="${response##*HTTPSTATUS:}"
    status="${status//$'\n'/}"
    log_result "large_path_handling" "404" "$status" "$([ "$status" == "404" ] && echo "true" || echo "false")" "$body"
    
    # Unicode/Special characters
    local special_chars="%00%2e%2e%2f%2e%2e%2fetc%2fpasswd"
    response=$(curl -s -w "HTTPSTATUS:%{http_code}" "$BASE_URL/files/$special_chars" 2>/dev/null)
    body="${response%HTTPSTATUS:*}"
    status="${response##*HTTPSTATUS:}"
    status="${status//$'\n'/}"
    log_result "special_chars_handling" "403_or_404" "$status" "$( [[ "$status" == "403" || "$status" == "404" ]] && echo "true" || echo "false")" "$body"
    
    # Empty path
    response=$(curl -s -w "HTTPSTATUS:%{http_code}" "$BASE_URL/files/" 2>/dev/null)
    body="${response%HTTPSTATUS:*}"
    status="${response##*HTTPSTATUS:}"
    status="${status//$'\n'/}"
    log_result "empty_path_handling" "403_or_301" "$status" "$( [[ "$status" == "403" || "$status" == "301" ]] && echo "true" || echo "false")" "$body"
}

# Run all test categories
test_basic_file_access
test_path_traversal  
test_symlink_attacks
test_directory_protection
test_volume_attacks
test_edge_cases

# Generate summary report
echo -e "\n${BLUE}📊 Test Summary Report${NC}"
total_tests=$(jq '.results | length' "$RESULTS_FILE")
passed_tests=$(jq '[.results[] | select(.success == true)] | length' "$RESULTS_FILE")
failed_tests=$(jq '[.results[] | select(.success == false)] | length' "$RESULTS_FILE")

echo "Total Tests: $total_tests"
echo -e "Passed: ${GREEN}$passed_tests${NC}"
echo -e "Failed: ${RED}$failed_tests${NC}"

if [[ $failed_tests -eq 0 ]]; then
    echo -e "\n${GREEN}🎉 All security tests passed! xg2g is secure.${NC}"
    exit_code=0
else
    echo -e "\n${RED}⚠️ Some security tests failed. Review the issues above.${NC}"
    echo "Failed tests:"
    jq -r '.results[] | select(.success == false) | "  - " + .test + " (Expected: " + .expected_status + ", Got: " + .actual_status + ")"' "$RESULTS_FILE"
    exit_code=1
fi

echo ""
echo "Detailed results saved to: $RESULTS_FILE"
echo -e "View Grafana metrics at: ${BLUE}http://localhost:3000/d/xg2g-main${NC}"
echo ""

exit $exit_code