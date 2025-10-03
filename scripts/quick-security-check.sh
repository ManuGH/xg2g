#!/bin/bash
# Quick security validation test
# Tests key security features against a running xg2g instance

BASE_URL="${1:-http://localhost:8080}"

echo "🔍 Quick Security Validation for xg2g"
echo "Target: $BASE_URL"
echo ""

# Test 1: Symlink escape attempt
echo -n "Testing symlink escape protection... "
response=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/files/../../etc/passwd")
if [[ "$response" == "403" ]]; then
    echo "✅ PASS (403 Forbidden)"
else
    echo "❌ FAIL (Got: $response, Expected: 403)"
fi

# Test 2: Path traversal attempt  
echo -n "Testing path traversal protection... "
response=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/files/../../../etc/passwd")
if [[ "$response" == "403" || "$response" == "404" ]]; then
    echo "✅ PASS ($response)"
else
    echo "❌ FAIL (Got: $response, Expected: 403/404)"
fi

# Test 3: Method restrictions
echo -n "Testing POST method blocking... "
response=$(curl -s -X POST -o /dev/null -w "%{http_code}" "$BASE_URL/files/test.txt")
if [[ "$response" == "405" ]]; then
    echo "✅ PASS (405 Method Not Allowed)"
else
    echo "❌ FAIL (Got: $response, Expected: 405)"
fi

# Test 4: Health check
echo -n "Testing service health... "
response=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/healthz")
if [[ "$response" == "200" ]]; then
    echo "✅ PASS (200 OK)"
else
    echo "❌ FAIL (Got: $response, Expected: 200)"
fi

echo ""
echo "Quick validation complete. Run './scripts/security-test.sh' for comprehensive testing."