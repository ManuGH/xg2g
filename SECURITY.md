<!-- markdownlint-disable MD025 -->
# Security Policy

## Supported Versions

We release security updates for the latest stable version only.

| Version | Supported          |
| ------- | ------------------ |
| latest  | ✅                 |
| < latest| ❌                 |

## Reporting a Vulnerability

If you discover a security vulnerability in xg2g, please report it responsibly:

### How to Report

Do NOT open a public GitHub issue. Instead, please email security details to:

- Email: security@xg2g.local (placeholder)
- PGP Key: Optional — link to a public key if available

### What to Include

Please include the following information:

1. Description: Clear description of the vulnerability
2. Impact: Potential impact and attack scenarios
3. Reproduction: Step-by-step instructions to reproduce
4. Affected versions: Which versions are affected
5. Proposed fix: (Optional) Suggested remediation

### Response Timeline

- Initial response: Within 48 hours
- Status update: Within 7 days
- Fix timeline: Varies by severity (see below)

### Severity Levels

| Severity  | Response Time           | Example                                        |
|-----------|--------------------------|------------------------------------------------|
| Critical  | 24-48 hours              | Remote code execution, authentication bypass   |
| High      | 3-7 days                 | Privilege escalation, sensitive data exposure  |
| Medium    | 14-30 days               | Denial of service, information disclosure      |
| Low       | 30-90 days               | Minor information leaks                        |

## Security Best Practices

When deploying xg2g:

### Network Security
- Run behind a reverse proxy (nginx, Caddy) with TLS
- Restrict API access via firewall rules
- Use internal networks for OpenWebIF connections
- Enable rate limiting on public endpoints

### Configuration
- Never commit `.env` files with credentials
- Use a read-only filesystem where possible (see distroless deployment)
- Run as non-root user (default in containers)
- Keep `XG2G_DATA` on a separate volume with size limits

### Monitoring
- Enable Prometheus metrics for security events
- Monitor `xg2g_file_requests_denied_total` for attack attempts
- Set up alerts for unusual API request patterns
- Review logs regularly for anomalies

### Updates
- Subscribe to GitHub releases for security updates
- Use automated vulnerability scanning (Dependabot, Renovate)
- Test updates in staging before production
- Keep base Docker images updated

## Security Features

xg2g includes several built-in security measures:

- Path traversal protection: `/files/*` endpoint validates paths
- Security metrics: Track denied requests and path traversal attempts
- Read-only containers: Distroless variant with minimal attack surface
- No shell access: Production images run without shell
- Hardened HTTP: Timeouts, header hardening, ETag/Cache-Control, and request ID tracing

## Disclosure Policy

- We follow coordinated disclosure principles
- Security fixes are released ASAP after verification
- CVE IDs may be requested for critical/high severity issues
- Credit is given to reporters (unless anonymity is requested)

## Known Limitations

- Authentication for mutating endpoints is recommended via reverse proxy if not configured
- No encryption of data at rest (use encrypted volumes)
- Logs may contain sensitive URLs (configure log sanitization)

## Past Security Issues

None reported to date.

---

Last updated: 2025-10-01
Policy version: 1.0
# Security Policy

## Threat Model

xg2g processes user-supplied configuration and serves files via HTTP. The primary attack vectors are:

### 1. Symlink Escape Attacks

- **Risk**: Symlinks pointing outside `XG2G_DATA` could expose system files
- **Mitigation**: Two-layer validation (startup + runtime) with realpath boundary checks
- **Implementation**: `filepath.EvalSymlinks()` validation against normalized `XG2G_DATA`

### 2. Path Traversal

- **Risk**: URL paths like `../../../etc/passwd` could access system files  
- **Mitigation**: Router normalization + realpath validation in file handler
- **Implementation**: HTTP 301 redirects for traversal attempts, boundary checks

### 3. Information Disclosure

- **Risk**: Error messages could leak file system structure or internal paths
- **Mitigation**: Unified "Forbidden"/"Not found" responses without technical details
- **Implementation**: Security logging with structured reason codes

## Security Features

### Startup Validation (`cmd/daemon/main.go`)

- **Symlink Policy**: `ensureDataDir()` validates data directory at startup
- **System Directory Protection**: Blocks `/etc`, `/usr`, `/var`, `/proc`, etc.
- **Realpath Resolution**: Normalizes symlinks to detect escape attempts

### Runtime Protection (`internal/api/http.go`)

- **Secure File Handler**: Custom handler replacing `http.FileServer`
- **Boundary Validation**: Every request validated against `XG2G_DATA` realpath
- **Method Restrictions**: Only GET/HEAD allowed (POST/PUT/DELETE → 405)
- **Directory Listing Prevention**: Directory access → 403 Forbidden

### Security Headers

- `X-Content-Type-Options: nosniff` - Prevent MIME type sniffing attacks

## Testing

### Security Test Suite

```bash
# Run startup symlink policy tests
go test ./cmd/daemon -run TestEnsureDataDirSymlinkPolicy -race -v

# Run HTTP handler security tests  
go test ./internal/api -run TestSecureFileHandlerSymlinkPolicy -race -v

# Run all security-tagged tests
go test ./... -tags security -race
```

### Test Coverage

- **A1-A5**: Startup validation (system dirs, broken symlinks, permissions)
- **B6-B12**: HTTP handler attacks (symlink escape, path traversal, encoding)
- **C13-C16**: Permission validation and error handling

### CI Security Gates

The security pipeline runs on every commit and includes:

- Symlink attack prevention validation
- Static security pattern enforcement  
- Security regression detection via repeated test runs
- Lint rules preventing `err.Error()` exposure in HTTP responses

## Reporting Security Issues

If you discover a security vulnerability, please report it to:

- **Contact**: security@manugh.dev (PGP key available on request)
- **Scope**: xg2g codebase, dependencies, and deployment configurations
- **Response Time**: We aim to respond within 48 hours

### Responsible Disclosure

1. Report the issue privately first
2. Allow 90 days for patch development  
3. Coordinate public disclosure timing
4. Credit will be provided in release notes

## Security Updates

Security patches are released as:

- **Critical**: Immediate patch releases with CVE assignment
- **High**: Next minor release (< 30 days)  
- **Medium/Low**: Next regular release

Subscribe to releases for security notifications: <https://github.com/ManuGH/xg2g/releases>

## Security Best Practices

### Deployment

- Run container as non-root user (UID 1000)
- Use minimal Alpine base image
- Mount data directory with restricted permissions (`0755`)
- Network isolation where possible

### Configuration  

- Validate `XG2G_DATA` points to dedicated directory
- Avoid symlinks in data directory tree
- Use dedicated non-privileged service account
- Enable security logging for monitoring

### Monitoring

- Monitor for frequent 403 responses (attack indicators)
- Alert on symlink escape attempts via structured logs
- Track file request patterns for anomalies

## Production Security Monitoring

### Prometheus Metrics (v1.2.0+)

Security events are tracked via comprehensive metrics:

```prometheus
# Security denials by attack type
xg2g_file_requests_denied_total{reason="boundary_escape"} 
xg2g_file_requests_denied_total{reason="method_not_allowed"}
xg2g_file_requests_denied_total{reason="path_traversal"}

# Successful file requests
xg2g_file_requests_allowed_total

# HTTP response codes for security analysis
xg2g_http_requests_total{status="403",endpoint="/files"}
xg2g_http_requests_total{status="405",endpoint="/files"}
```

### Real-time Alerting

Production monitoring includes automatic security alerts:

- **SymlinkAttackSpike**: >5 boundary escape attempts in 5 minutes
- **FileAccessDeniedSpike**: >10 access denials in 5 minutes  
- **HTTPErrorRateHigh**: >10% error rate indicates potential attack
- **HighLatencyP95**: Abnormal response times may indicate DoS

### Comprehensive Penetration Testing

Automated security validation suite:

```bash
# Full penetration test (20+ attack vectors)
./scripts/security-test.sh

# Quick CI/CD security check (4 core tests)
./scripts/quick-security-check.sh
```

**Test Coverage**:

- Path traversal (8 encoding variants)
- Symlink escape attempts (chains, directories)  
- HTTP method restrictions (POST/PUT/DELETE/PATCH)
- Concurrent attack resilience (10+ parallel requests)
- Edge cases (large paths, special characters)

### Security Configuration

Deploy with monitoring stack:

```bash
# Start production monitoring
docker-compose -f docker-compose.monitoring.yml up -d

# Verify security posture
./scripts/security-test.sh http://production:8080
```

**Dashboards**:

- Grafana security dashboard: <http://localhost:3000/d/xg2g-main>
- Real-time metrics: <http://localhost:9091/targets>
- Alert status: <http://localhost:9093/#/alerts>

## Dependencies

Security scanning includes:

- `govulncheck` for known CVE vulnerabilities
- Dependency audit via `go mod` security advisories
- Container base image security scanning
- **NEW**: Automated penetration testing via security test suite

Last updated: September 29, 2025 (v1.2.0 security monitoring update)
