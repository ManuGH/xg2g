# Security Audit Checklist

Comprehensive security audit checklist for xg2g deployments. Use this checklist for periodic security reviews, pre-production audits, and compliance verification.

## Audit Information

**Audit Date:** _______________
**Auditor:** _______________
**Deployment:** _______________ (Production/Staging/Development)
**xg2g Version:** _______________
**Audit Type:** □ Initial  □ Periodic  □ Pre-Release  □ Post-Incident

---

## Quick Reference

### Severity Levels

| Level | Symbol | Description | Action Required |
|-------|--------|-------------|-----------------|
| 🔴 Critical | ⚠️ | Immediate security risk | Fix immediately |
| 🟠 High | ⚠️ | Significant vulnerability | Fix within 7 days |
| 🟡 Medium | ⚠️ | Moderate security concern | Fix within 30 days |
| 🟢 Low | ℹ️ | Best practice recommendation | Plan for next sprint |

### Audit Sections

1. [Authentication & Authorization](#1-authentication--authorization)
2. [Network Security](#2-network-security)
3. [Data Protection](#3-data-protection)
4. [Container Security](#4-container-security)
5. [Dependency Security](#5-dependency-security)
6. [Logging & Monitoring](#6-logging--monitoring)
7. [Configuration Security](#7-configuration-security)
8. [Compliance](#8-compliance)

---

## 1. Authentication & Authorization

### 1.1 API Token Security

- [ ] **🔴 API token is set** (not using default or empty)
  ```bash
  # Check if token is configured
  echo $XG2G_API_TOKEN | wc -c  # Should be > 32
  ```
  - Severity: Critical
  - Finding: _______________
  - Remediation: Generate strong token with `openssl rand -base64 32`

- [ ] **🟠 Token is sufficiently strong**
  - Minimum 32 characters
  - Alphanumeric + special characters
  - Generated with cryptographic RNG
  - Finding: _______________

- [ ] **🟡 Token rotation policy exists**
  - Documented rotation schedule (e.g., every 90 days)
  - Process for emergency rotation
  - Finding: _______________

- [ ] **🟠 Token is not exposed in logs**
  ```bash
  grep -i "xg2g_api_token\|api.*token.*=" /var/log/xg2g/*.log
  ```
  - Should return no matches
  - Finding: _______________

- [ ] **🟠 Token is stored securely**
  - Not in git repository
  - Not in Docker images
  - Using secrets management (K8s Secrets/Vault/etc.)
  - Finding: _______________

### 1.2 OpenWebIF Authentication

- [ ] **🟡 OpenWebIF credentials use least privilege**
  - Read-only account when possible
  - Not using admin account
  - Finding: _______________

- [ ] **🟠 Credentials transmitted over HTTPS**
  - OpenWebIF URL uses https://
  - Certificate validation enabled
  - Finding: _______________

- [ ] **🟠 Credentials not in environment dumps**
  ```bash
  env | grep -i password
  ```
  - Finding: _______________

### 1.3 Access Control

- [ ] **🟠 Protected endpoints require authentication**
  ```bash
  curl -X POST http://localhost:8080/api/refresh
  # Should return 401 Unauthorized
  ```
  - Finding: _______________

- [ ] **🟢 Rate limiting configured on auth endpoints**
  - Finding: _______________

**Score:** _____ / 11
**Issues:** _____________________

---

## 2. Network Security

### 2.1 Network Exposure

- [ ] **🔴 xg2g not directly exposed to internet**
  ```bash
  # Check listening address
  netstat -tlnp | grep xg2g
  # Should show 127.0.0.1:8080 or internal IP
  ```
  - Finding: _______________

- [ ] **🟠 Reverse proxy configured**
  - nginx/Caddy/Traefik in front
  - TLS termination at proxy
  - Finding: _______________

- [ ] **🟠 Firewall rules restrict access**
  ```bash
  iptables -L -n | grep 8080
  ```
  - Only trusted IPs/networks allowed
  - Finding: _______________

### 2.2 TLS/HTTPS

- [ ] **🔴 HTTPS enabled** (not plain HTTP)
  - Finding: _______________

- [ ] **🟠 TLS version >= 1.2**
  ```bash
  nmap --script ssl-enum-ciphers -p 443 xg2g.example.com
  ```
  - Finding: _______________

- [ ] **🟠 Strong cipher suites configured**
  - No weak ciphers (RC4, DES, MD5)
  - Forward secrecy enabled (ECDHE)
  - Finding: _______________

- [ ] **🟡 HSTS header present**
  ```bash
  curl -I https://xg2g.example.com | grep -i strict-transport-security
  ```
  - Finding: _______________

- [ ] **🟡 Certificate auto-renewal configured**
  - Let's Encrypt certbot setup
  - Certificate expiry monitoring
  - Finding: _______________

### 2.3 Network Segmentation

- [ ] **🟡 xg2g in isolated network segment**
  - Not in DMZ
  - Separate from production databases
  - Finding: _______________

- [ ] **🟡 OpenWebIF on internal network only**
  - Not exposed to internet
  - Finding: _______________

**Score:** _____ / 10
**Issues:** _____________________

---

## 3. Data Protection

### 3.1 Data Directory Security

- [ ] **🟠 Data directory has restricted permissions**
  ```bash
  ls -ld /var/lib/xg2g/data
  # Should be 750 or 700
  ```
  - Finding: _______________

- [ ] **🟠 Data directory owned by xg2g user**
  ```bash
  ls -l /var/lib/xg2g
  # Should NOT be root:root
  ```
  - Finding: _______________

- [ ] **🟡 Data directory on encrypted filesystem**
  - LUKS/dm-crypt enabled
  - Finding: _______________

### 3.2 Sensitive Data Handling

- [ ] **🟠 Credentials not logged**
  ```bash
  grep -i "password\|secret\|token" /var/log/xg2g/*.log
  ```
  - Should only show masked values
  - Finding: _______________

- [ ] **🟠 URLs masked in logs**
  ```bash
  grep "http.*:.*@" /var/log/xg2g/*.log
  ```
  - Should show no credentials in URLs
  - Finding: _______________

### 3.3 File Handling

- [ ] **🟠 Path traversal prevention tested**
  ```bash
  curl http://localhost:8080/../etc/passwd
  # Should return 400/404, not file content
  ```
  - Finding: _______________

- [ ] **🟠 Symlink escape prevention verified**
  - Finding: _______________

**Score:** _____ / 7
**Issues:** _____________________

---

## 4. Container Security

### 4.1 Image Security

- [ ] **🟠 Running as non-root user**
  ```bash
  docker inspect xg2g | jq '.[0].Config.User'
  # Should be "nonroot" or numeric UID
  ```
  - Finding: _______________

- [ ] **🟠 Image scanned for vulnerabilities**
  ```bash
  trivy image ghcr.io/manugh/xg2g:latest
  ```
  - No HIGH or CRITICAL vulnerabilities
  - Finding: _______________

- [ ] **🟡 Using official/trusted base image**
  - gcr.io/distroless or similar
  - Not FROM scratch with unknown binaries
  - Finding: _______________

### 4.2 Runtime Security

- [ ] **🟠 Read-only root filesystem**
  ```bash
  docker inspect xg2g | jq '.[0].HostConfig.ReadonlyRootfs'
  # Should be true
  ```
  - Finding: _______________

- [ ] **🟠 Capabilities dropped**
  ```bash
  docker inspect xg2g | jq '.[0].HostConfig.CapDrop'
  # Should include "ALL"
  ```
  - Finding: _______________

- [ ] **🟠 No new privileges**
  ```bash
  docker inspect xg2g | jq '.[0].HostConfig.SecurityOpt'
  # Should include "no-new-privileges:true"
  ```
  - Finding: _______________

- [ ] **🟡 Resource limits configured**
  - CPU limit
  - Memory limit
  - Finding: _______________

### 4.3 Kubernetes Security (if applicable)

- [ ] **🟠 Pod Security Standards enforced**
  - Restricted or Baseline profile
  - Finding: _______________

- [ ] **🟠 Network policies configured**
  - Ingress/egress restrictions
  - Finding: _______________

- [ ] **🟡 Service account not default**
  - Dedicated service account
  - Minimal RBAC permissions
  - Finding: _______________

**Score:** _____ / 10
**Issues:** _____________________

---

## 5. Dependency Security

### 5.1 Go Dependencies

- [ ] **🟠 No known vulnerabilities**
  ```bash
  govulncheck ./...
  ```
  - Finding: _______________

- [ ] **🟡 Dependencies up to date**
  ```bash
  go list -u -m all
  ```
  - Finding: _______________

- [ ] **🟡 go.sum file integrity verified**
  ```bash
  go mod verify
  ```
  - Finding: _______________

### 5.2 Container Dependencies

- [ ] **🟠 Base image vulnerabilities scanned**
  ```bash
  grype ghcr.io/manugh/xg2g:latest
  ```
  - Finding: _______________

- [ ] **🟡 SBOM (Software Bill of Materials) available**
  ```bash
  syft ghcr.io/manugh/xg2g:latest
  ```
  - Finding: _______________

### 5.3 Update Management

- [ ] **🟡 Automated dependency updates configured**
  - Dependabot/Renovate enabled
  - Finding: _______________

- [ ] **🟢 Security advisory monitoring**
  - GitHub Security Advisories enabled
  - Finding: _______________

**Score:** _____ / 7
**Issues:** _____________________

---

## 6. Logging & Monitoring

### 6.1 Logging

- [ ] **🟡 Structured logging enabled**
  - JSON format
  - Consistent fields
  - Finding: _______________

- [ ] **🟠 Sensitive data not logged**
  - See section 3.2
  - Finding: _______________

- [ ] **🟡 Log aggregation configured**
  - Centralized logging (ELK/Loki/Splunk)
  - Finding: _______________

- [ ] **🟢 Log retention policy defined**
  - Minimum 90 days
  - Compliance requirements met
  - Finding: _______________

### 6.2 Monitoring

- [ ] **🟡 Metrics endpoint secured**
  ```bash
  curl http://localhost:9090/metrics
  # Should require auth or be localhost-only
  ```
  - Finding: _______________

- [ ] **🟠 Security alerts configured**
  - High error rate
  - Circuit breaker open
  - Authentication failures
  - Finding: _______________

- [ ] **🟡 Performance baseline established**
  - Normal behavior documented
  - Anomaly detection configured
  - Finding: _______________

### 6.3 Incident Response

- [ ] **🟡 Incident response plan exists**
  - Documented procedures
  - Contact information
  - Finding: _______________

- [ ] **🟢 Security incident logging**
  - Auth failures logged
  - Suspicious activity tracked
  - Finding: _______________

**Score:** _____ / 9
**Issues:** _____________________

---

## 7. Configuration Security

### 7.1 Configuration Management

- [ ] **🟠 No hardcoded secrets in code**
  ```bash
  grep -r "password.*=\|token.*=" --include="*.go" .
  ```
  - Finding: _______________

- [ ] **🟠 Configuration via environment/secrets**
  - Not in config files in git
  - Using secrets management
  - Finding: _______________

- [ ] **🟡 Configuration validation on startup**
  - Invalid config fails fast
  - Clear error messages
  - Finding: _______________

### 7.2 Default Settings

- [ ] **🔴 Default OWIBase removed** (already fixed)
  - No hardcoded private IPs
  - Finding: ✅ Fixed in a3349c6

- [ ] **🟠 Secure defaults for all settings**
  - No permissive defaults
  - Security-first configuration
  - Finding: _______________

### 7.3 Feature Flags

- [ ] **🟡 Dangerous features disabled by default**
  - HDHomeRun (optional)
  - Debug modes
  - Finding: _______________

**Score:** _____ / 6
**Issues:** _____________________

---

## 8. Compliance

### 8.1 Security Best Practices

- [ ] **🟢 Follows OWASP Top 10**
  - Injection prevention
  - Broken authentication prevention
  - Sensitive data exposure prevention
  - Finding: _______________

- [ ] **🟢 CIS Docker Benchmark compliance**
  ```bash
  docker-bench-security
  ```
  - Finding: _______________

### 8.2 Documentation

- [ ] **🟡 Security hardening guide followed**
  - SECURITY_HARDENING.md reviewed
  - Recommendations implemented
  - Finding: _______________

- [ ] **🟢 Security policy documented**
  - Vulnerability reporting process
  - Security contacts
  - Finding: _______________

### 8.3 Testing

- [ ] **🟡 Security tests in CI**
  - gosec scanning
  - govulncheck
  - Container scanning
  - Finding: _______________

- [ ] **🟢 Penetration testing performed**
  - External security audit
  - Findings remediated
  - Finding: _______________

**Score:** _____ / 6
**Issues:** _____________________

---

## Summary

### Scores by Section

| Section | Score | Percentage | Status |
|---------|-------|------------|--------|
| 1. Authentication | ___/11 | ___% | ___ |
| 2. Network Security | ___/10 | ___% | ___ |
| 3. Data Protection | ___/7 | ___% | ___ |
| 4. Container Security | ___/10 | ___% | ___ |
| 5. Dependency Security | ___/7 | ___% | ___ |
| 6. Logging & Monitoring | ___/9 | ___% | ___ |
| 7. Configuration Security | ___/6 | ___% | ___ |
| 8. Compliance | ___/6 | ___% | ___ |
| **Total** | **___/66** | **___%** | **___** |

### Overall Assessment

**Security Posture:** □ Excellent (>90%)  □ Good (75-90%)  □ Fair (60-75%)  □ Poor (<60%)

### Critical Issues (🔴)

1. _______________________________________________
2. _______________________________________________
3. _______________________________________________

### High Priority Issues (🟠)

1. _______________________________________________
2. _______________________________________________
3. _______________________________________________

### Recommendations

1. **Immediate Actions** (0-7 days):
   - _______________________________________________
   - _______________________________________________

2. **Short-term Actions** (7-30 days):
   - _______________________________________________
   - _______________________________________________

3. **Long-term Improvements** (30+ days):
   - _______________________________________________
   - _______________________________________________

---

## Appendix A: Testing Commands

### Quick Security Test Suite

```bash
#!/bin/bash
# security-test-suite.sh

echo "=== xg2g Security Test Suite ==="

# 1. Test authentication
echo "1. Testing authentication..."
curl -X POST http://localhost:8080/api/refresh
echo ""

# 2. Test path traversal
echo "2. Testing path traversal prevention..."
curl http://localhost:8080/../etc/passwd
echo ""

# 3. Test security headers
echo "3. Testing security headers..."
curl -I http://localhost:8080/healthz | grep -E "X-Content-Type|X-Frame|X-XSS"
echo ""

# 4. Test TLS configuration
echo "4. Testing TLS..."
nmap --script ssl-enum-ciphers -p 443 localhost
echo ""

# 5. Test container security
echo "5. Testing container security..."
docker inspect xg2g | jq '{User:.Config.User, ReadOnly:.HostConfig.ReadonlyRootfs, Caps:.HostConfig.CapDrop}'
echo ""

# 6. Test dependency vulnerabilities
echo "6. Testing dependencies..."
govulncheck ./...
echo ""

echo "=== Test Suite Complete ==="
```

### Automated Compliance Check

```bash
#!/bin/bash
# compliance-check.sh

score=0
total=0

check() {
    total=$((total + 1))
    if eval "$2"; then
        echo "✅ $1"
        score=$((score + 1))
    else
        echo "❌ $1"
    fi
}

echo "=== Automated Compliance Check ==="

check "API token set" '[ -n "$XG2G_API_TOKEN" ]'
check "Strong token (32+ chars)" '[ ${#XG2G_API_TOKEN} -ge 32 ]'
check "HTTPS enabled" 'netstat -tlnp | grep :443'
check "Running as non-root" '[ "$(docker inspect xg2g | jq -r ".[0].Config.User")" != "root" ]'
check "Read-only filesystem" '[ "$(docker inspect xg2g | jq -r ".[0].HostConfig.ReadonlyRootfs")" == "true" ]'
check "Data dir permissions" '[ "$(stat -c %a /var/lib/xg2g/data)" -le 750 ]'

echo ""
echo "Score: $score/$total ($(( score * 100 / total ))%)"

if [ $score -eq $total ]; then
    echo "🎉 All checks passed!"
    exit 0
else
    echo "⚠️  Some checks failed"
    exit 1
fi
```

---

## Appendix B: Remediation Templates

### Template: API Token Rotation

```bash
# 1. Generate new token
NEW_TOKEN=$(openssl rand -base64 32)

# 2. Update secrets
kubectl create secret generic xg2g-secrets \
  --from-literal=api-token="$NEW_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -

# 3. Rolling restart
kubectl rollout restart deployment/xg2g

# 4. Verify
kubectl logs -l app=xg2g | grep "configuration loaded"

# 5. Revoke old token (document in changelog)
echo "$(date): Rotated API token" >> /var/log/xg2g/security-audit.log
```

### Template: Vulnerability Remediation

```bash
# 1. Identify vulnerability
govulncheck ./... > vulns.txt

# 2. Update affected dependencies
go get -u <vulnerable-package>@<fixed-version>

# 3. Test
go test ./...

# 4. Build new image
docker build -t xg2g:patched .

# 5. Scan new image
trivy image xg2g:patched

# 6. Deploy
docker tag xg2g:patched xg2g:latest
docker push xg2g:latest
```

---

## Change Log

| Date | Version | Auditor | Changes |
|------|---------|---------|---------|
| 2025-10-23 | 1.0 | Initial | Initial checklist created |
| ___ | ___ | ___ | ___ |

---

**Document Owner:** Security Team
**Review Frequency:** Quarterly
**Next Review:** _______________
**Related Documents:**
- [SECURITY_HARDENING.md](./SECURITY_HARDENING.md)
- [CI_LOAD_TESTING.md](./CI_LOAD_TESTING.md)
- [TESTING_STRATEGY.md](./TESTING_STRATEGY.md)
