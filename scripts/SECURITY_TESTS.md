# xg2g Security Test Suite

This directory contains security testing tools for validating xg2g's file serving protections.

## Test Scripts

### `security-test.sh` - Comprehensive Security Test Suite

Complete penetration testing framework with 6 test categories:

1. **Basic File Access** - Valid requests, nonexistent files, method restrictions
2. **Path Traversal Attacks** - Various traversal patterns and encoding attempts  
3. **Symlink Escape Attacks** - Symlink-based directory escape attempts
4. **Directory Listing Protection** - Directory access and listing prevention
5. **High-Volume Attack Simulation** - Concurrent attack resilience testing
6. **Edge Cases** - Large paths, special characters, malformed requests

#### Usage

```bash
# Test against default local instance
./scripts/security-test.sh

# Test against custom target
./scripts/security-test.sh http://staging.example.com:8080

# Test with custom temp directory and concurrency
./scripts/security-test.sh http://localhost:8080 /tmp/my-tests 20
```

#### Output

- Real-time test results with colored pass/fail indicators
- JSON results file with detailed response data
- Summary report with pass/fail counts
- References to Grafana metrics dashboard

### `quick-security-check.sh` - Fast Security Validation  

Lightweight security validation for CI/CD pipelines and quick checks.

Tests core security features:

- Symlink escape protection (403 expected)
- Path traversal protection (403/404 expected)
- HTTP method restrictions (405 expected)
- Service health (200 expected)

#### Usage

```bash
# Quick validation of local instance
./scripts/quick-security-check.sh

# Quick validation of remote instance  
./scripts/quick-security-check.sh http://production.example.com:8080
```

## Test Categories Explained

### Path Traversal Patterns

The test suite validates protection against common traversal attacks:

- Basic patterns: `../`, `../../`, `../../../`
- Encoded variants: `%2e%2e%2f`, `%252F`
- Double encoding: `%252e%252e%252f`
- Alternative encodings: `%c0%af`, `....//`

### Symlink Attack Vectors

Tests symlink-based escape attempts:

- Direct symlinks pointing outside data directory
- Symlink chains (A → B → outside)
- Directory symlinks
- Mixed symlink/traversal combinations

### Expected Security Responses

| Attack Type | Expected Response | Meaning |
|-------------|------------------|---------|
| Path Traversal | 403 Forbidden | Security middleware blocks request |
| Symlink Escape | 403 Forbidden | Symlink resolution detects boundary violation |
| Invalid Method | 405 Method Not Allowed | HTTP method restriction enforced |
| Directory Access | 403/301 | Directory listing blocked or redirected |
| Nonexistent File | 404 Not Found | Normal file not found behavior |

## Metrics Integration

Security tests trigger metrics that can be monitored in Grafana:

- `xg2g_file_requests_denied_total{reason="boundary_escape"}` - Symlink/traversal blocks
- `xg2g_file_requests_denied_total{reason="method_not_allowed"}` - Method restrictions
- `xg2g_file_requests_allowed_total` - Successful file serves
- `xg2g_http_requests_total{status="403"}` - Security violations

## Continuous Integration

Add security testing to CI/CD pipelines:

```yaml
# .github/workflows/security.yml
- name: Security Tests
  run: |
    # Start xg2g in background
    XG2G_DATA=./testdata XG2G_OWI_BASE=http://stub XG2G_BOUQUET=test ./xg2g-daemon &
    sleep 2

    # Run security validation
    ./scripts/quick-security-check.sh

    # Optional: Full test suite
    ./scripts/security-test.sh
```

## Troubleshooting

### Common Issues

- **Connection refused**: Ensure xg2g is running on the target URL
- **jq not found**: Install jq for JSON result processing
- **Permission denied**: Ensure scripts are executable (`chmod +x scripts/*.sh`)

### Debugging Failed Tests

1. Check the JSON results file for detailed response bodies
2. Review Grafana dashboards for metrics during test execution  
3. Check xg2g logs for security event details
4. Verify test environment setup (directories, permissions)

### Custom Test Development

To add new security tests:

1. Add test function following naming pattern `test_category_name()`
2. Use `log_result()` helper for consistent result tracking
3. Include expected vs actual status code validation
4. Add test category to main execution flow
5. Update this README with new test descriptions

## Security Test Philosophy

These tests validate defense-in-depth security:

- **Application layer**: HTTP method restrictions, input validation
- **Middleware layer**: Path traversal detection and blocking  
- **Filesystem layer**: Symlink resolution and boundary checking
- **Monitoring layer**: Real-time security event tracking

The goal is comprehensive validation that xg2g properly protects against common web application security vulnerabilities while maintaining usability for legitimate file access.
